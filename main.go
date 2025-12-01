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

	"github.com/PaesslerAG/jsonpath"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	addr = flag.String("listen-address", ":9109", "The address to listen on for HTTP requests.")
)

var uris = []string{}

var uris_smd101 = []string{
	"/unit/name", // ok
	"/unit/temp/internal", // ok
	"/unit/cpu_usage", // ok
	"/unit/memory_usage", // ok
	"/unit/usage_user", // ok
	"/xtime/date", // ok
	"/xtime/timezone_offset", // ok
	"/unit/location", // ok
	"/player/1", // ok, player status info
	//state 0 is scheduled 11 is transfer skipped
	//"/player/1/uri", // "ch://8", oder direkt eine rtsp:// URI; Feld: "Playlist" in der WebUI
	//"/player/1/time", // like "00:00:35.429886000"
	//"/player/1/track_uri", // like "rtsp://128.130.88.10/extron1"; Feld "Source:" in der WebUI
	"/player/1/stream_statistics", // -> bitrates, packets ! of the current stream
	"/player/history/entries?count=1", // .date
}


var uris_smp400 = []string{
	"/unit/name",
	"/unit/temp/internal",
	"/unit/temp/board",
	"/unit/temp/cpu",
	"/unit/cpu_usage",
	"/unit/memory_usage",
	"/record/free_space",
	"/audio/dsp/oid/60002/v", // on smp400, left output meter
	"/audio/dsp/oid/60003/v", // on smp400, right output meter
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
        "/streamer/control/1/mode", //smp400 replaces /encoder/1/stream_enable; "2" for stream on, instead of "1" on smp300
        "/streamer/control/2/mode", //smp400 ..
        "/streamer/control/3/mode", //smp400 ..
	"/video/out/1/presets/layout/active",
	"/streamer/rtmp/1",
	"/streamer/rtmp/2",
	"/streamer/rtmp/3",
}

var uris_smp300 = []string{
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
//	"/audio/dsp/multiple_oid/v?oids=60000,60001",
	"/audio/dsp/oid/60000/v",
	"/audio/dsp/oid/60001/v",
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
	"/streamer/rtmp/1",
	"/streamer/rtmp/2",
	"/streamer/rtmp/3",
}

var sources = map[string]string{}

var sources_smd101 = map[string]string{
	"extron_temp_internal":                     `$[? @.meta.uri=="/unit/temp/internal"].result`,
	"extron_cpu_usage":                         `$[? @.meta.uri=="/unit/cpu_usage"].result[0]`,
	"extron_ram_usage":                         `$[? @.meta.uri=="/unit/memory_usage"].result`,
	"extron_internal_disk_used":                `$[? @.meta.uri=="/unit/usage_user"].result.used`,
	"extron_internal_disk_total":               `$[? @.meta.uri=="/unit/usage_user"].result.total`,
	"extron_internal_disk_free":                `$[? @.meta.uri=="/unit/usage_user"].result.available`,
	"extron_xtime_date":                        `$[? @.meta.uri=="/xtime/date"].result`,
	// smd "stopped|playing|paused"
	"extron_play_state":                        `$[? @.meta.uri=="/player/1"].result.play_state`,
	// like "00:00:35.429886000"
	"extron_player_time":                       `$[? @.meta.uri=="/player/1"].result.time`,
	// -> bitrates, packets of the current stream
	"extron_player_stream_audio_bitrate":       `$[? @.meta.uri=="/player/1/stream_statistics"].result.audio_bitrate_kbps`,
	"extron_player_stream_video_bitrate":       `$[? @.meta.uri=="/player/1/stream_statistics"].result.video_bitrate_kbps`,
	// .date, format 2025-11-25T16:04:53Z
	"extron_player_history_latest_date":        `$[? @.meta.uri=="/player/history/entries?count=1"].result[0].date`,
	// "ch://8", oder eine (rtsp):// URI -> 2float fails with IPs
	// # "extron_player_channel":                    `$[? @.meta.uri=="/player/1"].result.uri`,
	// # "extron_player_history_latest_uri":         `$[? @.meta.uri=="/player/history/entries?count=1"].result[0].uri`,
	// # "extron_player_track_uri":                  `$[? @.meta.uri=="/player/1"].result.track_uri`,
}

var sources_smp300 = map[string]string{
	"extron_temp_internal":                     `$[? @.meta.uri=="/unit/temp/internal"].result`,
	"extron_temp_board1":                       `$[? @.meta.uri=="/unit/temp/board1"].result`,
	"extron_temp_board2":                       `$[? @.meta.uri=="/unit/temp/board2"].result`,
	"extron_temp_cpu":                          `$[? @.meta.uri=="/unit/temp/cpu"].result`,
	"extron_temp_disk":                         `$[? @.meta.uri=="/tool/disktemperature"].result.RAW_VALUE`,
	"extron_fan_cpu1":                          `$[? @.meta.uri=="/unit/fan_speed"].result.fan_cpu1`,
	"extron_fan_board1":                        `$[? @.meta.uri=="/unit/fan_speed"].result.fan_board1`,
	"extron_cpu_usage":                         `$[? @.meta.uri=="/unit/cpu_usage"].result[0]`,
	"extron_ram_usage":                         `$[? @.meta.uri=="/unit/memory_usage"].result`,
	"extron_internal_disk_used":                `$[? @.meta.uri=="/record/free_space"].result.internal[0].used`,
	"extron_internal_disk_total":               `$[? @.meta.uri=="/record/free_space"].result.internal[0].total`,
	"extron_internal_disk_free":                `$[? @.meta.uri=="/record/free_space"].result.internal[0].free_space`,
  	"extron_audio_dsp_left":                    `$[? @.meta.uri=="/audio/dsp/oid/60000/v"].result`,
  	"extron_audio_dsp_right":                   `$[? @.meta.uri=="/audio/dsp/oid/60001/v"].result`,
	"extron_video_in_channel_a":                `$[? @.meta.uri=="/video/in/channel/1"].result.active_input`,
	"extron_video_in_channel_b":                `$[? @.meta.uri=="/video/in/channel/2"].result.active_input`,
	"extron_record_state":                      `$[? @.meta.uri=="/record/state"].result`,
	"extron_xtime_date":                        `$[? @.meta.uri=="/xtime/date"].result`,
	"extron_schedule_ingest_active":            `$[? @.meta.uri=="/schedule_ingest/active_service"].result.active`,
	"extron_publish_active":                    `$[? @.meta.uri=="/publish/active_service"].result.active`,
	"extron_schedule_state_upcoming":           `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 0 )].state`,
	"extron_schedule_state_transfer_skipped":   `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 11 )].state`,
	"extron_schedule_state_transfer_completed": `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 5 )].state`,
	"extron_schedule_state_transfer_no_method": `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 10 )].state`,
	"extron_encoder_1_stream_enabled":          `$[? @.meta.uri=="/encoder/1/stream_enable"].result`,
	"extron_encoder_2_stream_enabled":          `$[? @.meta.uri=="/encoder/2/stream_enable"].result`,
	"extron_encoder_3_stream_enabled":          `$[? @.meta.uri=="/encoder/3/stream_enable"].result`,
	"extron_video_out_1_layout":                `$[? @.meta.uri=="/video/out/1/presets/layout/active"].result.active_index`,
	"extron_streamer_1_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/1"].result.pub_control`,
	"extron_streamer_2_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/2"].result.pub_control`,
	"extron_streamer_3_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/3"].result.pub_control`,
}


var sources_smp400 = map[string]string{
	"extron_temp_internal":                     `$[? @.meta.uri=="/unit/temp/internal"].result`,
	"extron_temp_board1":                       `$[? @.meta.uri=="/unit/temp/board"].result`,
	"extron_temp_cpu":                          `$[? @.meta.uri=="/unit/temp/cpu"].result`,
	"extron_cpu_usage":                         `$[? @.meta.uri=="/unit/cpu_usage"].result[0]`,
	"extron_ram_usage":                         `$[? @.meta.uri=="/unit/memory_usage"].result`,
	"extron_internal_disk_used":                `$[? @.meta.uri=="/record/free_space"].result.internal[0].used`,
	"extron_internal_disk_total":               `$[? @.meta.uri=="/record/free_space"].result.internal[0].total`,
	"extron_internal_disk_free":                `$[? @.meta.uri=="/record/free_space"].result.internal[0].free_space`,
	"extron_audio_dsp_left":                    `$[? @.meta.uri=="/audio/dsp/oid/60002/v"].result`,
	"extron_audio_dsp_right":                   `$[? @.meta.uri=="/audio/dsp/oid/60003/v"].result`,
	"extron_video_in_channel_a":                `$[? @.meta.uri=="/video/in/channel/1"].result.active_input`,
	"extron_video_in_channel_b":                `$[? @.meta.uri=="/video/in/channel/2"].result.active_input`,
	"extron_record_state":                      `$[? @.meta.uri=="/record/state"].result.state`,
	"extron_xtime_date":                        `$[? @.meta.uri=="/xtime/date"].result`,
	"extron_schedule_ingest_active":            `$[? @.meta.uri=="/schedule_ingest/active_service"].result.active`,
	"extron_publish_active":                    `$[? @.meta.uri=="/publish/active_service"].result.active`,
	"extron_schedule_state_upcoming":           `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 0 )].state`,
	"extron_schedule_state_transfer_skipped":   `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 11 )].state`,
	"extron_schedule_state_transfer_completed": `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 5 )].state`,
	"extron_schedule_state_transfer_no_method": `$[? @.meta.uri=="/schedule/schedule?format=json&field=db_id,state"].result[?(@.state == 10 )].state`,
 	// extron1 is stream 1 on smp400 and stream 3 on smp300
  	"extron_encoder_3_stream_enabled":          `$[? @.meta.uri=="/streamer/control/1/mode"].result`,
 	// ch_a is stream 2 on smp400 and stream 1 on smp300
  	"extron_encoder_1_stream_enabled":          `$[? @.meta.uri=="/streamer/control/2/mode"].result`,
 	// ch_b is stream 3 on smp400 and stream 2 on smp300
  	"extron_encoder_2_stream_enabled":          `$[? @.meta.uri=="/streamer/control/3/mode"].result`,
	"extron_video_out_1_layout":                `$[? @.meta.uri=="/video/out/1/presets/layout/active"].result.active_index`,
	"extron_streamer_1_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/1"].result.pub_control`,
	"extron_streamer_2_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/2"].result.pub_control`,
	"extron_streamer_3_rtmp":                   `$[? @.meta.uri=="/streamer/rtmp/3"].result.pub_control`,
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

	// Login and get a cookie as authentication for followup requests
	target, err := url.Parse(paramTarget)
	target.Path = "/api/login"
	payload := r.Header.Get("Authorization")

	cookiereq, err := http.NewRequest("POST", target.String(), nil)
	if payload != "" {
		cookiereq.Header.Add("Authorization", payload)
	}
	cookieresp, err := client.Do(cookiereq)
	cookies := cookieresp.Cookies()

	defer cookieresp.Body.Close()

	if err != nil {
		log.Printf("Request failed: %s", cookieresp.Status)
		http.Error(w, "", cookieresp.StatusCode)
		return
	}

	// Get the model info to decide on SMP300 vs SMP400 vs SMD101 uris and sources
	target.Path = "/api/swis/resource/unit/model/name"
	modelreq, err := http.NewRequest("GET", target.String(), nil)
	if payload != "" {
		modelreq.Header.Add("Authorization", payload)
		for i := range cookies {
  			modelreq.AddCookie(cookies[i])
		}	
	}
	modelresp, err := client.Do(modelreq)

	defer modelresp.Body.Close()

	modelbytes, err := ioutil.ReadAll(modelresp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var modeljsonData interface{}
	json.Unmarshal([]byte(modelbytes), &modeljsonData)

	model, err := jsonpath.Get(`$.result`, modeljsonData)

	if "SMP 401" == model {
		sources = sources_smp400
		uris = uris_smp400
		log.Print("--> Using SMP400 uris and sources")
	} else if "SMD 101" == model {
		sources = sources_smd101
		uris = uris_smd101
		log.Print("--> Using SMD101 uris and sources")
	} else {
		sources = sources_smp300
		uris = uris_smp300
		log.Print("--> Using SMP300 uris and sources")
	}

	log.Print("Model: ", model)

	// Get the information from the SM(P|D) via one uri request
	target.Path = "/api/swis/resources"
	q := target.Query()
	q.Set("_dc", strconv.FormatInt(time.Now().Unix(), 10))
	for _, r := range uris {
		q.Add("uri", r)
	}
	target.RawQuery = q.Encode()

	req, err := http.NewRequest("GET", target.String(), nil)
	if payload != "" {
		req.Header.Add("Authorization", payload)
		for i := range cookies {
  			req.AddCookie(cookies[i])
		}	
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
	log.Print("jsonData: ", jsonData)	

	name, err := jsonpath.Get(`$[? @.meta.uri=="/unit/name"].result`, jsonData)
	name, err = jsonpath.Get("$[0]", name)
	location, err := jsonpath.Get(`$[? @.meta.uri=="/unit/location"].result`, jsonData)
	location, err = jsonpath.Get("$[0]", location)
	timezone_offset, err := jsonpath.Get(`$[? @.meta.uri=="/xtime/timezone_offset"].result`, jsonData)
	timezone_offset, err = jsonpath.Get("$[0]", timezone_offset)

   	log.Print("Handling: ", name, " ", location, " ", timezone_offset)

	for metricName, jsonPath := range sources {
		res, err := jsonpath.Get(jsonPath, jsonData)
		log.Print("\nMetric: ", metricName, " Res: ", res)
		if err != nil {
			log.Print("Metric skipped, jsonPath err: ", metricName, " Res: ", res)
			continue
		}
		
		if len(res.([]interface{})) == 1 { // un-array single point values
			log.Print("Metric is of length 1: ", metricName, " Res: ", res)
			res, err = jsonpath.Get("$[0]",res)
		}

		switch v := res.(type) {
		case string:
			log.Print("Metric is of type string: ", metricName, " Res: ", res)
			str := regex.ReplaceAllString(res.(string), "")
			log.Print("  after regex w/o string: ", metricName, " Res: ", str)
			if "extron_record_state" == metricName { // SMP 300 (SMP 400 does not return a string)
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
			if "extron_play_state" == metricName { // SMD 101
				if res == "playing" {
					str = "2"
				} else if res == "paused" {
					str = "1"
				} else if res == "stopped" {
					str = "0"
				} else {
					str = "-1"
				}
			}
			if "extron_player_time" == metricName { // SMD 101
				if res == "" {
					str = "0"
				}
			}
			number, err := strconv.ParseFloat(str, 64)
			if err != nil {
				http.Error(w, "Values could not be parsed to Float64", http.StatusInternalServerError)
				return
			}
			if "extron_xtime_date" == metricName {
				t, _ := time.Parse("Mon, 02 Jan 2006 15:4:5 -07:00", res.(string)+" "+timezone_offset.(string))
				number = float64(t.Unix())
			}
			if "extron_player_history_latest_date" == metricName {
				t, _ := time.Parse("2006-01-02T15:4:5Z -07:00", res.(string)+" "+timezone_offset.(string))
				number = float64(t.Unix())
			}
			res = number
			log.Print("Metric was string, is now number: ", metricName, " Res: ", res)
		case bool:
			if res.(bool) {
				res = float64(1)
			} else {
				res = float64(0)
			}
			log.Print("Metric is of type bool: ", metricName, " Res: ", res)
		case []interface{}:
			res = float64(len(v))
			log.Print("Metric is of type interface: ", metricName, " Res: ", res)
		default:
			res = v
			match, _ := regexp.MatchString("extron_encoder_._stream_enabled", metricName)
			if match {
				if res == float64(2) { // adapt "enabled" state == 2 on smp400 vs == 1 on smp300
					res = float64(1)
                        	}
			}
			match, _ = regexp.MatchString("extron_audio_dsp.*", metricName)
			if match {
				if res.(float64) > 0  { // adapt audio meter level > 0 on smp400 vs < 0 on smp300
					res = -res.(float64)
                        	}
			}
			match, _ = regexp.MatchString("extron_record_state", metricName)
			if match {
				if res.(float64) == 3  { // adapt "paused" state == 3 on smp400 vs == 1 on smp300
					res = res.(float64)-2
                        	} 
			}
			log.Print("Metric is of type default: ", metricName, " Res: ", res)
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
