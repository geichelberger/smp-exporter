package main

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"github.com/oliveagle/jsonpath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", ":9109", "The address to listen on for HTTP requests.")
)

var uris = []string{
	"/unit/name",
	"/unit/temp/internal",
	"/unit/temp/board1",
	"/unit/temp/board2",
	"/unit/temp/cpu",
	"/tool/disktemperature",
	"/unit/fan_speed",
	"/unit/cpu_usage",
	"/unit/memory_usage",
	"/record/free_space",
	"/audio/dsp/multiple_oid/v?oids=60000,60001",
	"/video/in/channel/1",
	"/video/in/channel/2",
	"/record/state",
	"/xtime/date",
	"/xtime/timezone_offset",
	"/schedule_ingest/active_service",
	"/publish/active_service",
	//state 0 is scheduled 11 is transfer skipped
	"/schedule/schedule?format=json&field=db_id,state",
	"/unit/location",
	"/encoder/1/stream_enable",
	"/encoder/2/stream_enable",
	"/encoder/3/stream_enable",
	"/video/out/1/presets/layout/active",
}

var sources = map[string]string{
	"extron_temp_internal":                     "$[1].result",
	"extron_temp_board1":                       "$[2].result",
	"extron_temp_board2":                       "$[3].result",
	"extron_temp_cpu":                          "$[4].result",
	"extron_temp_disk":                         "$[5].result.RAW_VALUE",
	"extron_fan_cpu1":                          "$[6].result.fan_cpu1",
	"extron_fan_board1":                        "$[6].result.fan_board1",
	"extron_cpu_usage":                         "$[7].result[0]",
	"extron_ram_usage":                         "$[8].result",
	"extron_internal_disk_used":                "$[9].result.internal[0].used",
	"extron_internal_disk_total":               "$[9].result.internal[0].total",
	"extron_internal_disk_free":                "$[9].result.internal[0].free_space",
	"extron_audio_dsp_left":                    "$[10].result.60000",
	"extron_audio_dsp_right":                   "$[10].result.60001",
	"extron_video_in_channel_a":                "$[11].result.active_input",
	"extron_video_in_channel_b":                "$[12].result.active_input",
	"extron_record_state":                      "$[13].result",
	"extron_xtime_date":                        "$[14,15].result", //"extron_xtime_timezone_offset":             "$[15].result",
	"extron_schedule_ingest_active":            "$[16].result.active",
	"extron_publish_active":                    "$[17].result.active",
	"extron_schedule_state_upcoming":           "$[18].result[?(@.state == 0 )].state",
	"extron_schedule_state_transfer_skipped":   "$[18].result[?(@.state == 11 )].state",
	"extron_schedule_state_transfer_completed": "$[18].result[?(@.state == 5 )].state",
	"extron_schedule_state_transfer_no_method": "$[18].result[?(@.state == 10 )].state", //unit/location 19
	"extron_encoder_1_stream_enabled":          "$[20].result",
	"extron_encoder_2_stream_enabled":          "$[21].result",
	"extron_encoder_3_stream_enabled":          "$[22].result",
	"extron_video_out_1_layout":                "$[23].result.active_index",
}

var regex = regexp.MustCompile("[^\\.0-9]+")

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
            <head><title>SMP Exporter</title></head>
            <body>
            <h1>SMP Exporter</h1>
            <p><a href="/probe">Run a probe</a></p>
            <p><a href="/metrics">Metrics</a></p>
            </body>
            </html>`))
	})
	flag.Parse()

	http.HandleFunc("/probe", probeHandler)
	http.Handle("/metrics", promhttp.Handler())
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func probeHandler(w http.ResponseWriter, r *http.Request) {

	getParams := r.URL.Query()

	probeSuccessGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_success",
		Help: "Displays whether or not the probe was a success",
	})
	probeDurationGauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "probe_duration_seconds",
		Help: "Returns how long the probe took to complete in seconds",
	})

	start := time.Now()
	registry := prometheus.NewRegistry()
	registry.MustRegister(probeSuccessGauge)
	registry.MustRegister(probeDurationGauge)

	tr := &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   time.Second * 10,
		Transport: tr,
	}

	paramTarget := getParams.Get("target")
	if paramTarget == "" {
		http.Error(w, "Target parameter is missing", 400)
		return
	}

	target, err := url.Parse(paramTarget)
	target.Path = "/api/swis/resources"
	payload := r.Header.Get("Authorization")

	q := target.Query()
	q.Set("_dc", strconv.FormatInt(time.Now().Unix(), 10))
	for _, r := range uris {
		q.Add("uri", r)
	}
	target.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", target.String(), nil)
	if payload != "" {
		req.Header.Add("Authorization", payload)
	}
	resp, err := client.Do(req)

	if err != nil {
		log.Printf("Request failed: %s", err.Error())
		http.Error(w, err.Error(), http.StatusGatewayTimeout)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("Request failed: %s", resp.Status)
		http.Error(w, "", resp.StatusCode)
		return
	}

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var jsonData interface{}
	json.Unmarshal([]byte(bytes), &jsonData)

	name, err := jsonpath.JsonPathLookup(jsonData, "$[0].result")
	location, err := jsonpath.JsonPathLookup(jsonData, "$[19].result")

	for metricName, jsonPath := range sources {
		res, err := jsonpath.JsonPathLookup(jsonData, jsonPath)
		if err != nil {
			continue
		}

		switch v := res.(type) {
		case string:
			str := regex.ReplaceAllString(res.(string), "")
			if "extron_record_state" == metricName {
				if res == "recording" {
					str = "2"
				} else if res == "paused" {
					str = "1"
				} else if res == "stopped" {
					str = "0"
				} else {
					str = "-1"
				}
			}
			number, err := strconv.ParseFloat(str, 64)
			if err != nil {
				http.Error(w, "Values could not be parsed to Float64", http.StatusInternalServerError)
				return
			}
			res = number
		case bool:
			if res.(bool) {
				res = float64(1)
			} else {
				res = float64(0)
			}
		case []interface{}:
			if "extron_xtime_date" == metricName {
				t, _ := time.Parse("Mon, 02 Jan 2006 15:4:5 -07:00", v[0].(string)+" "+v[1].(string))
				res = float64(t.Unix())
			} else {
				res = float64(len(v))
			}
		default:
			res = v
		}

		number, ok := res.(float64)
		if !ok {
			http.Error(w, "Values could not be parsed to Float64", http.StatusInternalServerError)
			return
		}
		valueGauge := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: metricName,
			Help: "Retrieved value",
		},
			[]string{"unit_name", "unit_location"})

		registry.MustRegister(valueGauge)
		valueGauge.With(prometheus.Labels{"unit_name": name.(string), "unit_location": location.(string)}).Set(number)
	}
	probeSuccessGauge.Set(1)

	probeDurationGauge.Set(time.Since(start).Seconds())
	h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
	h.ServeHTTP(w, r)
}
