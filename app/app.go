package app

import (
	"compress/gzip"
	"context"
	"errors"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/natefinch/lumberjack"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/acme/autocert"
)

const RootDomain = "asset.unko.in"

const (
	AccessLogPath = "./log"
	WwwPath       = "./www"
	RootDataPath  = "./data"
)

type Unixtime time.Time

func (ts *Unixtime) UnmarshalJSON(data []byte) error {
	i, err := strconv.ParseInt(string(data), 10, 64)
	t := time.Unix(i, 0)
	*ts = Unixtime(t)
	return err
}
func (ts Unixtime) MarshalJSON() ([]byte, error) {
	return strconv.AppendInt([]byte(nil), time.Time(ts).Unix(), 10), nil
}
func (ts *Unixtime) UnmarshalBinary(data []byte) error {
	t := time.Time(*ts)
	err := t.UnmarshalBinary(data)
	*ts = Unixtime(t)
	return err
}
func (ts Unixtime) MarshalBinary() ([]byte, error) {
	return time.Time(ts).MarshalBinary()
}

type Indicator struct {
	Date     Unixtime `json:"date"`
	Open     float64  `json:"open"`
	High     float64  `json:"high"`
	Volume   float64  `json:"volume"`
	Low      float64  `json:"low"`
	Close    float64  `json:"close"`
	Adjclose float64  `json:"adjclose"`
}

type Meta struct {
	Currency          string   `json:"currency"`
	Symbol            string   `json:"symbol"`
	ExchangeName      string   `json:"exchangeName"`
	InstrumentType    string   `json:"instrumentType"`
	FirstTradeDate    Unixtime `json:"firstTradeDate"`
	RegularMarketTime Unixtime `json:"regularMarketTime"`
	DataGranularity   string   `json:"dataGranularity"`
	Range             string   `json:"range"`
}

type Store struct {
	Name       string      `json:"name"`
	Code       string      `json:"code"`
	Type       uint        `json:"type"`
	Meta       Meta        `json:"meta"`
	Indicators []Indicator `json:"indicators"`
}

const (
	assetTypeTosho = iota // 東証
	assetTypeFund         // 投資信託
	assetTypeEnd
)

type asset struct {
	name string
	typ  uint
	code string
}

type Srv struct {
	s *http.Server
	f func(s *http.Server) error
}

type App struct {
	wg sync.WaitGroup
}

var log *zap.SugaredLogger
var gzipContentTypeList = []string{
	"text/html",
	"text/css",
	"text/javascript",
	"text/plain",
	"application/json",
}

var myassets = []asset{
	{
		name: "楽天",
		typ:  assetTypeTosho,
		code: "4755.T",
	},
	{
		name: "ENEOSホールディングス",
		typ:  assetTypeTosho,
		code: "5020.T",
	},
}

func init() {
	//logger, err := zap.NewDevelopment()
	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	log = logger.Sugar()
	rand.Seed(time.Now().UnixNano())
}

func New() *App {
	return &App{}
}

func (app *App) Run(ctx context.Context) error {
	ctx, exitch := app.startExitManageProc(ctx)

	// 各種データ取得
	app.wg.Add(1)
	go app.getDataProc(ctx)

	monich := make(chan ResultMonitor)
	rich := make(chan ResponseInfo, 32)

	app.wg.Add(1)
	go app.serverMonitoringProc(ctx, rich, monich)

	// URL設定
	http.Handle("/api/unko.in/1/monitor", &GetMonitoringHandler{ch: monich})
	http.Handle("/", http.FileServer(http.Dir(WwwPath)))

	ghfunc, err := gziphandler.GzipHandlerWithOpts(gziphandler.CompressionLevel(gzip.BestSpeed), gziphandler.ContentTypes(gzipContentTypeList))
	if err != nil {
		exitch <- struct{}{}
		log.Infow("サーバーハンドラの作成に失敗しました。", "error", err)
		return app.shutdown(ctx)
	}
	h := MonitoringHandler(ghfunc(http.DefaultServeMux), rich)

	// サーバ情報
	sl := []Srv{
		{
			s: &http.Server{Addr: ":8080", Handler: h},
			f: func(s *http.Server) error { return s.ListenAndServe() },
		},
		{
			s: &http.Server{Handler: h},
			f: func(s *http.Server) error { return s.Serve(autocert.NewListener(RootDomain)) },
		},
	}
	for _, s := range sl {
		s := s // ローカル化
		app.wg.Add(1)
		go s.startServer(&app.wg)
	}
	// シャットダウン管理
	return app.shutdown(ctx, sl...)
}

func (srv Srv) startServer(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Infow("Srv.startServer", "Addr", srv.s.Addr)
	// サーバ起動
	err := srv.f(srv.s)
	// サーバが終了した場合
	if err != nil {
		if err == http.ErrServerClosed {
			log.Infow("サーバーがシャットダウンしました。", "error", err, "Addr", srv.s.Addr)
		} else {
			log.Warnw("サーバーが落ちました。", "error", err)
		}
	}
}

func (app *App) shutdown(ctx context.Context, sl ...Srv) error {
	// シグナル等でサーバを中断する
	<-ctx.Done()
	// シャットダウン処理用コンテキストの用意
	sctx, scancel := context.WithCancel(context.Background())
	defer scancel()
	for _, srv := range sl {
		app.wg.Add(1)
		go func(ctx context.Context, srv *http.Server) {
			sctx, sscancel := context.WithTimeout(ctx, time.Second*10)
			defer func() {
				sscancel()
				app.wg.Done()
			}()
			err := srv.Shutdown(sctx)
			if err != nil {
				log.Warnw("サーバーの終了に失敗しました。", "error", err)
			} else {
				log.Infow("サーバーの終了に成功しました。", "Addr", srv.Addr)
			}
		}(sctx, srv.s)
	}
	// サーバーの終了待機
	app.wg.Wait()
	return log.Sync()
}

func (app *App) startExitManageProc(ctx context.Context) (context.Context, chan<- struct{}) {
	exitch := make(chan struct{}, 1)
	ectx, cancel := context.WithCancel(ctx)
	app.wg.Add(1)
	go func(ctx context.Context, ch <-chan struct{}) {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig,
			syscall.SIGHUP,
			syscall.SIGINT,
			syscall.SIGTERM,
			syscall.SIGQUIT,
			os.Interrupt,
			os.Kill,
		)
		defer func() {
			signal.Stop(sig)
			cancel()
			app.wg.Done()
		}()

		select {
		case <-ctx.Done():
			log.Infow("Cancel from parent")
		case s := <-sig:
			log.Infow("Signal!!", "signal", s)
		case <-ch:
			log.Infow("Exit command!!")
		}
	}(ectx, exitch)
	return ectx, exitch
}

func (app *App) getDataProc(ctx context.Context) {
	defer app.wg.Done()
	tc := time.NewTicker(10 * time.Minute)
	defer tc.Stop()
	i := 0
	for {
		select {
		case <-ctx.Done():
			log.Infow("getDataProc終了")
			return
		case <-tc.C:
			as := myassets[i]
			i = (i + 1) % len(myassets)
			getPrice(ctx, as)
			log.Infow("価格情報取得", "name", as.name, "code", as.code)
		}
	}
}

func getPrice(ctx context.Context, as asset) {
	tctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	switch as.typ {
	case assetTypeTosho:
		storePriceJpx(tctx, as)
	case assetTypeFund:
		// 未実装
	default:
	}
}

// サーバお手軽監視用
func (app *App) serverMonitoringProc(ctx context.Context, rich <-chan ResponseInfo, monich chan<- ResultMonitor) {
	defer app.wg.Done()
	// logrotateの設定がめんどくせーのでアプリでやる
	// https://github.com/uber-go/zap/blob/master/FAQ.md
	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&lumberjack.Logger{
			Filename:   filepath.Join(AccessLogPath, "access.log"),
			MaxSize:    100, // megabytes
			MaxBackups: 100,
			MaxAge:     7,    // days
			Compress:   true, // disabled by default
		}),
		zap.InfoLevel,
	))
	defer logger.Sync()
	res := ResultMonitor{}
	resmin := ResultMonitor{}
	tc := time.NewTicker(time.Minute)
	defer tc.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Infow("serverMonitoringProc終了")
			return
		case monich <- resmin:
		case ri := <-rich:
			ela := ri.end.Sub(ri.start)
			res.ResponseCount++
			res.ResponseTimeSum += ela
			if ri.status < 400 {
				res.ResponseCodeOkCount++
			} else {
				res.ResponseCodeNgCount++
			}
			// アクセスログ出力
			logger.Info("-",
				zap.String("addr", ri.addr),
				zap.String("host", ri.host),
				zap.String("method", ri.method),
				zap.String("uri", ri.uri),
				zap.String("protocol", ri.protocol),
				zap.Int("status", ri.status),
				zap.Int("size", ri.size),
				zap.String("ua", ri.userAgent),
				zap.Duration("elapse", ela),
			)
		case <-tc.C:
			resmin = res
			res = ResultMonitor{}
		}
	}
}

func makedir(p string) error {
	st, err := os.Stat(p)
	if err == nil {
		if st.IsDir() == false {
			return errors.New("作るフォルダと同じ名前のファイルがありました。")
		}
	} else {
		mkdirerr := os.MkdirAll(p, 0666)
		if mkdirerr != nil {
			return mkdirerr
		}
	}
	return nil
}

func createAssetFolderPath(as asset) string {
	return filepath.Join(RootDataPath, as.code)
}
