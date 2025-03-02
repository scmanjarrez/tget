package tget

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/vbauerster/mpb/v8"
)

//go:embed torrc
var TorrcTemplate string

type TorGet struct {
}

var Version = "v0.3.2"

func PrepareRequest(req *http.Request, headers []string, cookies, useragent, body string) {
	for _, h := range headers {
		split := strings.SplitN(h, ":", 2)
		k := split[0]
		v := strings.TrimSpace(split[1])
		req.Header.Add(k, v)
	}
	if cookies != "" {
		req.Header.Add("Cookie", cookies)
	}
	if len(body) > 0 {
		req.Body = io.NopCloser(strings.NewReader(body))
	}

	if useragent != "" {
		req.Header.Set("User-Agent", useragent)
	}
}

func DownloadUrl(c *http.Client, req *http.Request, outPath string, followRedir, tryContinue, overwrite bool, bar *mpb.Bar) {
	currentSize := 0
	if stat, err := os.Stat(outPath); err == nil {
		// TODO: implement proper resume with etag/filehash/Ifrange etc, check if accpet-range is supported, etc
		if tryContinue {
			currentSize := stat.Size()
			//log.Printf("%v found on disk, user asked to attempt a resume (%d)\n", outPath, currentSize)
			req.Header.Set("range", fmt.Sprintf("bytes=%d-", currentSize))
		}

		//in case overwrite is set, start from the beginning anyway
		if overwrite {
			currentSize = 0
			req.Header.Del("range")
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()
	//log.Printf("client downloading %v to %v (%d) %v (size: %v)\n", req.URL.String(), outPath, resp.StatusCode, resp.Header.Get("Location"), resp.Header.Get("content-length"))

	if (resp.StatusCode >= 300 && resp.StatusCode <= 399) || resp.Header.Get("location") != "" {
		if followRedir {
			redirectUrl, err := resp.Location()
			if err != nil {
				bar.Abort(false)
				return
			}

			// create a new GET request to follow the redirect
			req.URL = redirectUrl
			resp, err = c.Do(req)
			if err != nil {
				bar.Abort(false)
				return
			}
			defer resp.Body.Close()
		} else {
			//log.Println("aborting")
			bar.Abort(false)
			return
		}
	}

	out, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Println(err)
		return
	}
	defer out.Close()
	out.Seek(int64(currentSize), 0)

	totalBytes, err := strconv.Atoi(resp.Header.Get("content-length"))
	if err != nil {
		log.Println(req.URL, "couldn't read content-lenght")
		totalBytes = -1
	}

	//log.Printf("%v size is %d\n", req.URL, totalBytes)
	bar.SetTotal(int64(totalBytes), false)

	pw := bar.ProxyWriter(out)
	pr := bar.ProxyReader(resp.Body)
	defer pr.Close()
	defer pw.Close()

	_, err = io.Copy(pw, pr)
	if err != nil && err != io.EOF {
		//_, err := io.ReadAll(pr)
		//log.Println("body:", body)
		log.Println("copy error:", err)
		bar.Abort(false)
	}

	bar.SetTotal(-1, true) // set as complete
	//log.Println(out.Name(), "done")
}

func GetFilename(file string) string {
	attempt := 0
	currentFile := file
	for {
		_, err := os.Stat(currentFile)
		if os.IsNotExist(err) {
			return currentFile
		}
		attempt++
		currentFile = fmt.Sprintf("%s.%d", file, attempt)
	}
}

func GetFreePorts(n int) (ports []int, err []error) {
	for i := 0; i < n; i++ {
		if a, e := net.ResolveTCPAddr("tcp", "localhost:0"); e == nil {
			var l *net.TCPListener
			if l, e = net.ListenTCP("tcp", a); e == nil {
				defer l.Close()
				ports = append(ports, l.Addr().(*net.TCPAddr).Port)
			} else {
				err = append(err, e)
			}
		} else {
			err = append(err, e)
		}
	}

	return ports, err
}

func ChunkBy[T any](a []T, n int) [][]T {
	if n <= 0 {
		return [][]T{}
	}

	batches := make([][]T, 0, n)

	var size, lower, upper int
	l := len(a)

	for i := 0; i < n; i++ {
		lower = i * l / n
		upper = ((i + 1) * l) / n
		size = upper - lower

		a, batches = a[size:], append(batches, a[0:size:size])
	}
	return batches
}
