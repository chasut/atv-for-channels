package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	tunerLock sync.Mutex

	tuners = []tuner{
		{
			url:   "http://10.0.0.28/0.ts",
			name:   "tuner1",
		},
		{
			url:   "http://10.0.0.29/0.ts",
			name:   "tuner3",
		},
		{
			url:   "http://10.0.0.31/0.ts",
			name:   "tuner0",
		},
		{
			url:   "http://10.0.0.30/live/stream0",
			name:   "tuner2",
		},
	}
)

type tuner struct {
	url              string
	name             string
	active           bool
}

// status page type
type ExportedTuner struct {
	Name    string
	Url     string
	Active  bool
}

type reader struct {
	io.ReadCloser
	t       *tuner
	channel string
	started bool
}

func init() {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 5 * time.Second
	transport.DialContext = (&net.Dialer{
		Timeout: 5 * time.Second,
	}).DialContext
	http.DefaultClient.Transport = transport
}

func (r *reader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
	}
	return r.ReadCloser.Read(p)
}

func (r *reader) Close() error {
	stopplayer(r.t.name)
	tunerLock.Lock()
	r.t.active = false
	tunerLock.Unlock()
	return r.ReadCloser.Close()
}

func execute(args ...string) error {
	t0 := time.Now()
	log.Printf("Running %v", args)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	log.Printf("Finished running %v in %v", args[0], time.Since(t0))
	return err
}

func tuneplayer(tunername, channel string) bool{
	t0 := time.Now()
	log.Printf("Started tuning TMSID %v on %v", channel, tunername)
	client := &http.Client{}
	var data = strings.NewReader("{\"entity_id\": \"media_player." + tunername + "\", \"media_content_type\": \"url\", \"media_content_id\": \"spectrumTV://watch.spectrum.net/livetv/" + channel + "\"}")
	req, err := http.NewRequest("POST", "http://10.0.0.22:8123/api/services/media_player/play_media", data)
	if err != nil {
		log.Printf("[ERR] Failed to create tune script: %v", err)
	}
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI0MmY1YWNlMzNiNDQ0MThlYWFiYjIzZmMyZTE1Zjg0ZSIsImlhdCI6MTcwMDY4OTUxNSwiZXhwIjoyMDE2MDQ5NTE1fQ.ir4u10uOP3FweZ469HdpXszAMaE45c1onbJzQnTf4XU")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ERR] %v Failed to tune Tuner %v, retrying", err, tunername)
		return false
	} else if resp.StatusCode != 200 {
		log.Printf("Tuning call failed on Tuner %v, retrying. [ERR] %v", tunername, resp.Status)
		return false
	}
	log.Printf("Finished tuning in %v", time.Since(t0))
	return true	
}

func stopplayer(tunername string) {
	t0 := time.Now()
	log.Printf("Stopping tuner %v", tunername)
	client := &http.Client{}
	var data = strings.NewReader("{\"entity_id\": \"remote." + tunername + "\", \"command\": \"menu\"}")
	req, err := http.NewRequest("POST", "http://10.0.0.22:8123/api/services/remote/send_command", data)
	if err != nil {
		log.Printf("[ERR] Failed to create stop tune script: %v", err)
	}
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiI0MmY1YWNlMzNiNDQ0MThlYWFiYjIzZmMyZTE1Zjg0ZSIsImlhdCI6MTcwMDY4OTUxNSwiZXhwIjoyMDE2MDQ5NTE1fQ.ir4u10uOP3FweZ469HdpXszAMaE45c1onbJzQnTf4XU")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[ERR] Tuner stop call failed: %v\n%v", err, resp.Status)
	}
	log.Printf("Stopped tuner %v in %v", tunername, time.Since(t0))	
}

func tune(idx, channel string) (io.ReadCloser, error) {
	tunerLock.Lock()
	defer tunerLock.Unlock()

	var t *tuner
	log.Printf("tune for %v %v", idx, channel)
	if idx == "" || idx == "auto" {
		for i, ti := range tuners {
			if ti.active {
				continue
			}
			t = &tuners[i]
	    var chkTune bool = tuneplayer(t.name, channel)
	    if !chkTune {
	      continue
	    }
			break
		}
	} else {
		i, _ := strconv.Atoi(idx)
		if i < len(tuners) && i >= 0 {
			t = &tuners[i]
		}
	}
	if t == nil {
		return nil, fmt.Errorf("tuner not available")
	}

 
	resp, err := http.Get(t.url)
	if err != nil {
		log.Printf("[ERR] Failed to fetch source: %v", err)
		return nil, err
	} else if resp.StatusCode != 200 {
		log.Printf("[ERR] Failed to fetch source: %v", resp.Status)
		return nil, fmt.Errorf("invalid response: %v", resp.Status)
	}

	t.active = true
	return &reader{
		ReadCloser: resp.Body,
		channel:    channel,
		t:          t,
	}, nil

}

func run() error {
  gin.SetMode(gin.ReleaseMode)
  r := gin.New()
  r.Use(
      gin.LoggerWithWriter(gin.DefaultWriter, "/api/status"),
      gin.Recovery(),
  )	
	r.SetTrustedProxies(nil)
	r.GET("/play/tuner:tuner/:channel", func(c *gin.Context) {
		tuner := c.Param("tuner")
		channel := c.Param("channel")

		c.Header("Transfer-Encoding", "identity")
		c.Header("Content-Type", "video/mp2t")
		c.Writer.WriteHeaderNow()
		c.Writer.Flush()

		reader, err := tune(tuner, channel)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}

		defer func() {
			reader.Close()
		}()

		io.Copy(c.Writer, reader)
	})

	r.GET("/api/status", apiStatusHandler)

	return r.Run(":7654")
}

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

//Tuner Status
func apiStatusHandler(c *gin.Context) {

	tunerLock.Lock()
	exportedTuners := make([]ExportedTuner, len(tuners))
	for i, t := range tuners {
		exportedTuners[i] = ExportedTuner{
			Name:    t.name,
			Url:     t.url,
			Active:  t.active,
		}
	}
	tunerLock.Unlock()

	// Response with JSON
	c.JSON(http.StatusOK, gin.H{
		"Tuners":        exportedTuners,
	})
}
