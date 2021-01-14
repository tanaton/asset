package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type TradingPeriod struct {
	Timezone  string   `json:"timezone"`
	Start     Unixtime `json:"start"`
	End       Unixtime `json:"end"`
	Gmtoffset int64    `json:"gmtoffset"`
}

type Finance struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Currency             string   `json:"currency"`
				Symbol               string   `json:"symbol"`
				ExchangeName         string   `json:"exchangeName"`
				InstrumentType       string   `json:"instrumentType"`
				FirstTradeDate       Unixtime `json:"firstTradeDate"`
				RegularMarketTime    Unixtime `json:"regularMarketTime"`
				Gmtoffset            int64    `json:"gmtoffset"`
				Timezone             string   `json:"timezone"`
				ExchangeTimezoneName string   `json:"exchangeTimezoneName"`
				RegularMarketPrice   float64  `json:"regularMarketPrice"`
				ChartPreviousClose   float64  `json:"chartPreviousClose"`
				PriceHint            int64    `json:"priceHint"`
				CurrentTradingPeriod struct {
					Pre     TradingPeriod `json:"pre"`
					Regular TradingPeriod `json:"regular"`
					Post    TradingPeriod `json:"post"`
				} `json:"currentTradingPeriod"`
				DataGranularity string   `json:"dataGranularity"`
				Range           string   `json:"range"`
				ValidRanges     []string `json:"validRanges"`
			} `json:"meta"`
			Timestamp  []Unixtime `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []float64 `json:"open"`
					High   []float64 `json:"high"`
					Volume []float64 `json:"volume"`
					Low    []float64 `json:"low"`
					Close  []float64 `json:"close"`
				} `json:"quote"`
				Adjclose []struct {
					Adjclose []float64 `json:"adjclose"`
				} `json:"adjclose"`
			} `json:"indicators"`
		} `json:"result"`
		Error string `json:"error,omitempty"`
	} `json:"chart"`
}

func getHiashiURL(code string) string {
	return fmt.Sprintf("https://query2.finance.yahoo.com/v7/finance/chart/%s?range=10y&interval=1d", code)
}

func getTukiashiURL(code string) string {
	return fmt.Sprintf("https://query2.finance.yahoo.com/v7/finance/chart/%s?range=10y&interval=1mo", code)
}

func getJpxIndicator(ctx context.Context, u string) (*Finance, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var fin Finance
	{
		dec := json.NewDecoder(resp.Body)
		if err := dec.Decode(&fin); err != nil {
			return nil, err
		}
	}
	return &fin, nil
}

func getPriceJpx(ctx context.Context, as asset) {
	u := getHiashiURL(as.code)
	fin, err := getJpxIndicator(ctx, u)
	if err != nil {
		log.Warnw("通信に失敗しました。", "url", u, "error", err)
		return
	}
	if len(fin.Chart.Result) <= 0 {
		log.Warnw("取得したデータが不明なフォーマットでした。len(fin.Chart.Result) <= 0", "url", u)
		return
	}
	res := fin.Chart.Result[0]
	if len(res.Indicators.Quote) <= 0 {
		log.Warnw("取得したデータが不明なフォーマットでした。len(res.Indicators.Quote) <= 0", "url", u)
		return
	}
	if len(res.Timestamp) != len(res.Indicators.Quote[0].Open) {
		log.Warnw("データの時間数とデータの数が異なります。", "url", u)
		return
	}

	q := res.Indicators.Quote[0]
	s := Store{
		Name: as.name,
		Code: as.code,
		Type: as.typ,
		Meta: Meta{
			Currency:          res.Meta.Currency,
			Symbol:            res.Meta.Symbol,
			ExchangeName:      res.Meta.ExchangeName,
			InstrumentType:    res.Meta.InstrumentType,
			FirstTradeDate:    res.Meta.FirstTradeDate,
			RegularMarketTime: res.Meta.RegularMarketTime,
		},
		Indicators: make([]Indicator, 0, len(res.Timestamp)),
	}
	for i, date := range res.Timestamp {
		s.Indicators = append(s.Indicators, Indicator{
			Date:   date,
			Open:   q.Open[i],
			High:   q.High[i],
			Volume: q.Volume[i],
			Low:    q.Low[i],
			Close:  q.Close[i],
		})
	}

	dir := filepath.Join(RootDataPath, as.code)
	if err := makedir(dir); err != nil {
		log.Warnw("データ保存用フォルダの作成に失敗しました。", "url", u, "error", err)
		return
	}
	fp, err := os.Create(filepath.Join(dir, fmt.Sprintf("%d.json", time.Time(s.Meta.RegularMarketTime).Unix())))
	if err != nil {
		log.Warnw("データ保存用ファイルの作成に失敗しました。", "url", u, "error", err)
		return
	}
	defer fp.Close()
	bw := bufio.NewWriterSize(fp, 128*1024)
	b := bytes.Buffer{}
	w := io.MultiWriter(bw, &b)
	enc := json.NewEncoder(w)
	if err := enc.Encode(&s); err != nil {
		log.Warnw("データ保存用のJSONエンコードに失敗しました。", "url", u, "error", err)
		return
	}
	bw.Flush()
}
