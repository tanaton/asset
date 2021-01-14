package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
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

func getShuashiURL(code string) string {
	return fmt.Sprintf("https://query2.finance.yahoo.com/v7/finance/chart/%s?range=10y&interval=1w", code)
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
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&fin); err != nil {
		return nil, err
	}
	return &fin, nil
}

func financeToStore(as asset, fin *Finance) (*Store, error) {
	if len(fin.Chart.Result) <= 0 {
		return nil, errors.New("取得したデータが不明なフォーマットでした。len(fin.Chart.Result) <= 0")
	}
	res := fin.Chart.Result[0]
	if len(res.Indicators.Quote) <= 0 {
		return nil, errors.New("取得したデータが不明なフォーマットでした。len(res.Indicators.Quote) <= 0")
	}
	if len(res.Timestamp) != len(res.Indicators.Quote[0].Open) {
		return nil, errors.New("データの時間数とデータの数が異なります。")
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
			DataGranularity:   res.Meta.DataGranularity,
			Range:             res.Meta.Range,
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
	return &s, nil
}

func createIndicatorPath(as asset) (string, error) {
	dir := filepath.Join(createAssetFolderPath(as), "indicator")
	if err := makedir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

func storePriceJpx(ctx context.Context, as asset) {
	u := getHiashiURL(as.code)
	fin, err := getJpxIndicator(ctx, u)
	if err != nil {
		log.Warnw("通信に失敗しました。", "url", u, "error", err)
		return
	}
	s, err := financeToStore(as, fin)
	if err != nil {
		log.Warnw("変換に失敗しました。", "url", u, "error", err)
		return
	}
	dir, err := createIndicatorPath(as)
	if err != nil {
		log.Warnw("データ保存用フォルダの作成に失敗しました。", "url", u, "error", err)
		return
	}
	fp, err := os.Create(filepath.Join(dir, "day.json"))
	if err != nil {
		log.Warnw("データ保存用ファイルの作成に失敗しました。", "url", u, "error", err)
		return
	}
	defer fp.Close()
	bw := bufio.NewWriterSize(fp, 128*1024)
	enc := json.NewEncoder(bw)
	if err := enc.Encode(&s); err != nil {
		log.Warnw("データ保存用のJSONエンコードに失敗しました。", "url", u, "error", err)
		return
	}
	bw.Flush()
}
