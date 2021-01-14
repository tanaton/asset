package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"time"
)

type GetMonitoringHandler struct {
	ch <-chan ResultMonitor
}

type GetIndicatorHandler struct {
	path   string
	data   []byte
	update <-chan bool
}

func (h *GetMonitoringHandler) getResultMonitor(ctx context.Context) (ResultMonitor, error) {
	var res ResultMonitor
	lctx, lcancel := context.WithTimeout(ctx, time.Second*3)
	defer lcancel()
	select {
	case <-lctx.Done():
		return res, errors.New("timeout")
	case res = <-h.ch:
		log.Debugw("受信！ getResultMonitor", "data", res)
	}
	return res, nil
}

func (h *GetMonitoringHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	res, err := h.getResultMonitor(r.Context())
	if err == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		err := json.NewEncoder(w).Encode(res)
		if err != nil {
			log.Warnw("JSON出力に失敗しました。", "error", err, "path", r.URL.Path)
		}
	} else {
		http.Error(w, "データ取得に失敗しました。", http.StatusInternalServerError)
	}
}

func (h *GetIndicatorHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	select {
	case <-h.update:
		fp, err := os.Open(h.path)
		if err != nil {
			http.Error(w, "データ取得に失敗しました。", http.StatusInternalServerError)
			return
		}
		defer fp.Close()
		buf := bytes.Buffer{}
		_, rerr := buf.ReadFrom(fp)
		if rerr != nil {
			http.Error(w, "データ取得に失敗しました。", http.StatusInternalServerError)
			return
		}
		h.data = buf.Bytes()
	default:
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(h.data)
}
