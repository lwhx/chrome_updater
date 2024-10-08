/*
Package fetch for download, provide high performance download

use Goroutine to parallel download, use WaitGroup to do concurrency control.
*/
package main

import (
	"fmt"
	"fyne.io/fyne/v2/widget"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// FileFlag save file flag
//
// FileMode save file mode
const (
	FileFlag = os.O_WRONLY | os.O_CREATE
	FileMode = 0644
)

// WaitPool implement request pool to enhance performance
var (
	// WaitPool = sync.WaitGroup{}
	downloadedBytes int64
)

// GoroutineDownload will download form requestURL.
// example:
//
//	requestURL := "http://xxx"
//	GoroutineDownload(requestURL, 20, 10*1024*1024, 30)
func GoroutineDownload(sd *SettingsData, requestURL, fileName string, poolSize, chunkSize, timeout, fileSize int64, downloadProgress *widget.ProgressBar, wg *sync.WaitGroup) {
	var index, start int64

	if !strings.HasPrefix(requestURL, "http") {
		requestURL = "http://" + requestURL
	}
	requestURL = strings.TrimSpace(requestURL)

	// open file
	f, err := os.OpenFile(fileName, FileFlag, FileMode)
	if err != nil {
		logger.Errorf("open error:%+v\n", err)
		return
	}
	defer f.Close()
	pool := make(chan int64, (fileSize/chunkSize)+1)
	for index = 0; index < poolSize; index++ {
		go func() {
			// recover
			defer func() {
				if err2 := recover(); err2 != nil {
					logger.Errorf("panic error: %+v, stack:%s", err2, debug.Stack())
				}
			}()

			// loop download until finish
			for {
				start, err = downloadChunkToFile(sd, requestURL, pool, f, chunkSize, timeout, fileSize, downloadProgress, wg)
				if err != nil {
					logger.Errorf("fetch chunck start:%d error:%+v\n", start, err)
					pool <- start
				} else {
					break
				}
				logger.Debugln("start loop download again")
			}
		}()
	}

	for start = 0; start < fileSize; start += chunkSize {
		wg.Add(1)
		pool <- start
	}

	wg.Wait()
	// 关闭文件
	err = f.Close()
	if err != nil {
		logger.Errorf("关闭文件错误：%+v\n", err)
	}
}

func downloadChunkToFile(sd *SettingsData, requestURL string, pool chan int64, f *os.File, chunkSize, timeout int64, fileSize int64, downloadProgress *widget.ProgressBar, wg *sync.WaitGroup) (start int64, err error) {
	client := &http.Client{Timeout: time.Second * time.Duration(timeout)}
	if sd != nil {
		ghProxy := getString(sd.ghProxy)
		if ghProxy != "" {
			if getString(sd.proxyType) == "GH-PROXY" {
				requestURL = pathJoin(ghProxy, requestURL)
			} else {
				urli := url.URL{}
				urlproxy, _ := urli.Parse(ghProxy)
				client.Transport = &http.Transport{Proxy: http.ProxyURL(urlproxy)}
			}
		}
	}
	chunkRequest, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		logger.Errorf("create request error:%+v\n", err)
		return
	}

	var resp *http.Response
	var body []byte
	var written int
	for {
		start = <-pool
		chunkRequest.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, start+chunkSize-1))
		resp, err = client.Do(chunkRequest)
		if err != nil {
			logger.Errorf("send request error:%+v\n", err)
			return
		}

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			_ = resp.Body.Close()
			logger.Errorf("read response error:%+v\n", err)
			return
		}

		written, err = f.WriteAt(body, start)
		if err != nil {
			_ = resp.Body.Close()
			wg.Done()
			logger.Errorf("write file error:%+v\n", err)
			return
		}
		if downloadProgress != nil {
			atomic.AddInt64(&downloadedBytes, int64(written))
			currentPercent := float64(downloadedBytes) / float64(fileSize)
			//_ = bar.Add(written)
			downloadProgress.SetValue(currentPercent * 0.9)
		}
		_ = resp.Body.Close()

		// echo chunk will down one.
		wg.Done()
	}
}
