package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/nats-io/nats.go"
	"github.com/srcfl/sourceful-edge-node/internal/messaging"
	"github.com/srcfl/sourceful-edge-node/internal/modbus"
)

// ProbeRequest is received from Hugin via NATS.
type ProbeRequest struct {
	ID      string          `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ReplyTo string          `json:"reply_to,omitempty"`
}

// ProbeResponse is returned to Hugin.
type ProbeResponse struct {
	ID      string          `json:"id"`
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// StartProbeHandler subscribes to Hugin probe requests on NATS
// and executes them locally. This turns the binary into an edge node
// that can be remotely controlled by another Hugin instance.
func StartProbeHandler(nc *nats.Conn, serial string) error {
	if nc == nil {
		return fmt.Errorf("NATS not connected")
	}

	subject := messaging.HuginProbeSubject(serial)
	_, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		handleProbeRequest(nc, msg)
	})
	if err != nil {
		return err
	}

	log.Printf("Probe handler active on %s", subject)
	return nil
}

func handleProbeRequest(nc *nats.Conn, msg *nats.Msg) {
	var req ProbeRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		respondProbe(nc, msg, req, ProbeResponse{Error: "invalid request"})
		return
	}

	log.Printf("[probe] %s id=%s", req.Method, req.ID)

	var resp ProbeResponse
	resp.ID = req.ID

	switch req.Method {
	case "tcp_test":
		resp = probeExecTCPTest(req)
	case "detect_device":
		resp = probeExecDetect(req)
	case "read_registers":
		resp = probeExecReadRegisters(req)
	case "scan_register_range":
		resp = probeExecScanRange(req)
	case "network_scan":
		resp = probeExecNetworkScan(req)
	case "rest_fetch":
		resp = probeExecRESTFetch(req)
	case "rest_inspect":
		resp = probeExecRESTInspect(req)
	case "mqtt_connect":
		resp = probeExecMQTTConnect(req)
	case "mqtt_subscribe":
		resp = probeExecMQTTSubscribe(req)
	case "list_devices":
		resp = ProbeResponse{ID: req.ID, Success: true, Result: json.RawMessage(`{"count":0,"devices":[]}`)}
	default:
		resp = ProbeResponse{ID: req.ID, Error: "unknown method: " + req.Method}
	}

	respondProbe(nc, msg, req, resp)
}

func respondProbe(nc *nats.Conn, msg *nats.Msg, req ProbeRequest, resp ProbeResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	// Try reply_to first (for leaf node compatibility)
	if req.ReplyTo != "" {
		nc.Publish(req.ReplyTo, data)
		return
	}
	if msg.Reply != "" {
		msg.Respond(data)
		return
	}
}

func probeExecTCPTest(req ProbeRequest) ProbeResponse {
	var p struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Port == 0 {
		p.Port = 502
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), 2*time.Second)
	open := err == nil
	if conn != nil {
		conn.Close()
	}
	data, _ := json.Marshal(map[string]any{"host": p.Host, "port": p.Port, "open": open})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecDetect(req ProbeRequest) ProbeResponse {
	var p struct {
		Host    string `json:"host"`
		Port    int    `json:"port"`
		SlaveID int    `json:"slave_id"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Port == 0 {
		p.Port = 502
	}
	if p.SlaveID == 0 {
		p.SlaveID = 1
	}

	out := map[string]any{"host": p.Host, "port": p.Port, "slave_id": p.SlaveID}

	// TCP check
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(p.Host, strconv.Itoa(p.Port)), 2*time.Second)
	if err != nil {
		out["detected"] = false
		out["error"] = "port not open"
		data, _ := json.Marshal(out)
		return ProbeResponse{ID: req.ID, Success: true, Result: data}
	}
	conn.Close()

	// Modbus read SunSpec
	client, err := modbus.NewTCPClientWithTimeout(p.Host, p.Port, byte(p.SlaveID), 3*time.Second)
	if err != nil {
		out["detected"] = false
		out["error"] = err.Error()
		data, _ := json.Marshal(out)
		return ProbeResponse{ID: req.ID, Success: true, Result: data}
	}
	defer client.Close()

	raw, err := client.ReadHoldingRegisters(40000, 2)
	if err != nil {
		out["detected"] = false
		out["error"] = "modbus read failed: " + err.Error()
		data, _ := json.Marshal(out)
		return ProbeResponse{ID: req.ID, Success: true, Result: data}
	}

	out["detected"] = true
	values := modbus.BytesToRegisters(raw)
	sunspec := len(values) >= 2 && values[0] == 0x5375 && values[1] == 0x6e53
	out["sunspec"] = sunspec

	if sunspec {
		if mfr, err := client.ReadHoldingRegisters(40004, 16); err == nil {
			regs := modbus.BytesToRegisters(mfr)
			var b []byte
			for _, v := range regs {
				b = append(b, byte(v>>8), byte(v))
			}
			out["vendor"] = strings.TrimRight(string(b), "\x00 ")
		}
		if mdl, err := client.ReadHoldingRegisters(40020, 16); err == nil {
			regs := modbus.BytesToRegisters(mdl)
			var b []byte
			for _, v := range regs {
				b = append(b, byte(v>>8), byte(v))
			}
			out["model"] = strings.TrimRight(string(b), "\x00 ")
		}
	}

	data, _ := json.Marshal(out)
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecReadRegisters(req ProbeRequest) ProbeResponse {
	var p struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		UnitID       int    `json:"unit_id"`
		Address      int    `json:"address"`
		Count        int    `json:"count"`
		RegisterType string `json:"register_type"`
		Holding      *bool  `json:"holding"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Port == 0 {
		p.Port = 502
	}
	if p.Count == 0 {
		p.Count = 1
	}
	regType := p.RegisterType
	if regType == "" {
		regType = "holding"
		if p.Holding != nil && !*p.Holding {
			regType = "input"
		}
	}

	client, err := modbus.NewTCPClientWithTimeout(p.Host, p.Port, byte(p.UnitID), 3*time.Second)
	if err != nil {
		return ProbeResponse{ID: req.ID, Error: err.Error()}
	}
	defer client.Close()

	var raw []byte
	if regType == "input" {
		raw, err = client.ReadInputRegisters(uint16(p.Address), uint16(p.Count))
	} else {
		raw, err = client.ReadHoldingRegisters(uint16(p.Address), uint16(p.Count))
	}
	if err != nil {
		return ProbeResponse{ID: req.ID, Error: err.Error()}
	}

	values := modbus.BytesToRegisters(raw)
	intVals := make([]int, len(values))
	for i, v := range values {
		intVals[i] = int(v)
	}

	data, _ := json.Marshal(map[string]any{
		"host": p.Host, "port": p.Port, "register_type": regType,
		"address": p.Address, "values": intVals,
	})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecScanRange(req ProbeRequest) ProbeResponse {
	var p struct {
		Host         string `json:"host"`
		Port         int    `json:"port"`
		UnitID       int    `json:"unit_id"`
		Start        int    `json:"start"`
		End          int    `json:"end"`
		RegisterType string `json:"register_type"`
		Holding      *bool  `json:"holding"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Port == 0 {
		p.Port = 502
	}
	regType := p.RegisterType
	if regType == "" {
		regType = "holding"
		if p.Holding != nil && !*p.Holding {
			regType = "input"
		}
	}
	if p.End-p.Start > 1000 {
		p.End = p.Start + 1000
	}

	client, err := modbus.NewTCPClientWithTimeout(p.Host, p.Port, byte(p.UnitID), 3*time.Second)
	if err != nil {
		return ProbeResponse{ID: req.ID, Error: err.Error()}
	}
	defer client.Close()

	type entry struct {
		Address int   `json:"address"`
		Values  []int `json:"values"`
	}
	var found []entry
	chunk := 50
	for addr := p.Start; addr <= p.End; addr += chunk {
		count := chunk
		if addr+count-1 > p.End {
			count = p.End - addr + 1
		}
		var raw []byte
		if regType == "input" {
			raw, err = client.ReadInputRegisters(uint16(addr), uint16(count))
		} else {
			raw, err = client.ReadHoldingRegisters(uint16(addr), uint16(count))
		}
		if err != nil {
			continue
		}
		for i, v := range modbus.BytesToRegisters(raw) {
			if v != 0 {
				found = append(found, entry{Address: addr + i, Values: []int{int(v)}})
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	data, _ := json.Marshal(map[string]any{
		"scanned_range": fmt.Sprintf("%d-%d", p.Start, p.End),
		"register_type": regType, "non_zero": found, "count": len(found),
	})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecNetworkScan(req ProbeRequest) ProbeResponse {
	var p struct {
		Subnet  string `json:"subnet"`
		Ports   []int  `json:"ports"`
		StartIP int    `json:"start_ip"`
		EndIP   int    `json:"end_ip"`
		Timeout int    `json:"timeout_ms"`
	}
	json.Unmarshal(req.Params, &p)

	if p.Subnet == "" {
		// Auto-detect
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
				if v4 := ipn.IP.To4(); v4 != nil {
					parts := strings.Split(v4.String(), ".")
					if len(parts) == 4 {
						p.Subnet = parts[0] + "." + parts[1] + "." + parts[2]
						break
					}
				}
			}
		}
	}
	if p.Subnet == "" || len(p.Ports) == 0 {
		return ProbeResponse{ID: req.ID, Error: "subnet and ports required"}
	}
	if p.StartIP <= 0 {
		p.StartIP = 1
	}
	if p.EndIP <= 0 || p.EndIP > 254 {
		p.EndIP = 254
	}
	timeout := time.Duration(p.Timeout) * time.Millisecond
	if timeout <= 0 {
		timeout = 300 * time.Millisecond
	}

	type result struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	type job struct {
		host string
		port int
	}

	jobs := make(chan job, 256)
	results := make(chan result, 256)
	var wg sync.WaitGroup

	for w := 0; w < 32; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				conn, err := net.DialTimeout("tcp", net.JoinHostPort(j.host, strconv.Itoa(j.port)), timeout)
				if err == nil {
					conn.Close()
					results <- result{Host: j.host, Port: j.port}
				}
			}
		}()
	}

	go func() {
		for _, port := range p.Ports {
			for i := p.StartIP; i <= p.EndIP; i++ {
				jobs <- job{host: fmt.Sprintf("%s.%d", p.Subnet, i), port: port}
			}
		}
		close(jobs)
	}()
	go func() { wg.Wait(); close(results) }()

	var found []result
	for r := range results {
		found = append(found, r)
	}

	data, _ := json.Marshal(map[string]any{
		"subnet": p.Subnet + ".0/24", "ports": p.Ports, "found": found, "count": len(found),
	})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecRESTFetch(req ProbeRequest) ProbeResponse {
	var p struct {
		URL    string `json:"url"`
		Method string `json:"method"`
	}
	json.Unmarshal(req.Params, &p)
	if p.URL == "" {
		return ProbeResponse{ID: req.ID, Error: "url required"}
	}
	if p.Method == "" {
		p.Method = "GET"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	httpReq, _ := http.NewRequest(p.Method, p.URL, nil)
	resp, err := client.Do(httpReq)
	if err != nil {
		return ProbeResponse{ID: req.ID, Error: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 32*1024))
	data, _ := json.Marshal(map[string]any{"status_code": resp.StatusCode, "body": string(body)})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecRESTInspect(req ProbeRequest) ProbeResponse {
	var p struct {
		BaseURL string `json:"base_url"`
	}
	json.Unmarshal(req.Params, &p)
	if p.BaseURL == "" {
		return ProbeResponse{ID: req.ID, Error: "base_url required"}
	}
	base := strings.TrimRight(p.BaseURL, "/")
	endpoints := []string{"/", "/api", "/status", "/info", "/data", "/rpc", "/shelly", "/meter/0", "/emeter/0"}
	client := &http.Client{Timeout: 5 * time.Second}
	type ep struct {
		Path   string `json:"path"`
		Status int    `json:"status_code"`
		Body   string `json:"body"`
	}
	var found []ep
	for _, e := range endpoints {
		r, err := client.Get(base + e)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4*1024))
		r.Body.Close()
		if r.StatusCode < 400 {
			found = append(found, ep{Path: e, Status: r.StatusCode, Body: string(body)})
		}
	}
	data, _ := json.Marshal(map[string]any{"base_url": base, "responding": len(found), "endpoints": found})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecMQTTConnect(req ProbeRequest) ProbeResponse {
	var p struct {
		Broker   string `json:"broker"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Broker == "" {
		return ProbeResponse{ID: req.ID, Error: "broker required"}
	}
	if p.Port == 0 {
		p.Port = 1883
	}
	broker := fmt.Sprintf("tcp://%s:%d", p.Broker, p.Port)
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID("hugin-probe").SetConnectTimeout(5 * time.Second).SetAutoReconnect(false)
	if p.Username != "" {
		opts.SetUsername(p.Username)
	}
	if p.Password != "" {
		opts.SetPassword(p.Password)
	}
	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(5 * time.Second) {
		return ProbeResponse{ID: req.ID, Error: "connection timed out"}
	}
	if tok.Error() != nil {
		return ProbeResponse{ID: req.ID, Error: tok.Error().Error()}
	}
	c.Disconnect(250)
	data, _ := json.Marshal(map[string]any{"connected": true, "broker": broker})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}

func probeExecMQTTSubscribe(req ProbeRequest) ProbeResponse {
	var p struct {
		Broker      string `json:"broker"`
		Port        int    `json:"port"`
		Username    string `json:"username"`
		Password    string `json:"password"`
		Topic       string `json:"topic"`
		DurationSec int    `json:"duration_secs"`
	}
	json.Unmarshal(req.Params, &p)
	if p.Broker == "" || p.Topic == "" {
		return ProbeResponse{ID: req.ID, Error: "broker and topic required"}
	}
	if p.Port == 0 {
		p.Port = 1883
	}
	if p.DurationSec <= 0 {
		p.DurationSec = 5
	}
	if p.DurationSec > 30 {
		p.DurationSec = 30
	}
	broker := fmt.Sprintf("tcp://%s:%d", p.Broker, p.Port)
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID("hugin-sub").SetConnectTimeout(5 * time.Second).SetAutoReconnect(false)
	if p.Username != "" {
		opts.SetUsername(p.Username)
	}
	if p.Password != "" {
		opts.SetPassword(p.Password)
	}
	var mu sync.Mutex
	type sample struct {
		Topic   string `json:"topic"`
		Payload string `json:"payload"`
	}
	var samples []sample
	opts.SetDefaultPublishHandler(func(_ mqtt.Client, msg mqtt.Message) {
		mu.Lock()
		defer mu.Unlock()
		if len(samples) < 50 {
			payload := string(msg.Payload())
			if len(payload) > 2048 {
				payload = payload[:2048] + "..."
			}
			samples = append(samples, sample{Topic: msg.Topic(), Payload: payload})
		}
	})
	c := mqtt.NewClient(opts)
	tok := c.Connect()
	if !tok.WaitTimeout(5 * time.Second) || tok.Error() != nil {
		return ProbeResponse{ID: req.ID, Error: "connect failed"}
	}
	defer c.Disconnect(250)
	c.Subscribe(p.Topic, 0, nil)
	time.Sleep(time.Duration(p.DurationSec) * time.Second)
	mu.Lock()
	result := samples
	mu.Unlock()
	data, _ := json.Marshal(map[string]any{"topic_pattern": p.Topic, "message_count": len(result), "sample_messages": result})
	return ProbeResponse{ID: req.ID, Success: true, Result: data}
}
