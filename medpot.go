package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-ini/ini"
	"github.com/mozillazg/request"
	"go.uber.org/zap"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	CONN_HOST = "0.0.0.0"
	CONN_PORT = "2575"
	CONN_TYPE = "tcp"
)

func check(e error) {
	if e != nil {
		panic(e)
	}
}

/*
	read config from EWS poster for DTAGs Early warning system and T-Pot
*/
func readConfig() (string, string, string, string) {

	cfg, err := ini.Load("/etc/ews.cfg")
	if err != nil {
		fmt.Printf("Fail to read file: %v", err)
		os.Exit(1)
	}

	target := cfg.Section("EWS").Key("rhost_first").String()
	user := cfg.Section("EWS").Key("username").String()
	password := cfg.Section("EWS").Key("token").String()
	nodeid := cfg.Section("GLASTOPFV3").Key("nodeid").String()
	nodeid = strings.Replace(nodeid, "glastopfv3-", "medpot-", -1)
	return target, user, password, nodeid

}

func post(target string, user string, password string, nodeid string, myTime string, port string, ip string, encoded string) {

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	c := &http.Client{Transport: tr}
	req := request.NewRequest(c)

	dat := readFile("ews.xml")
	body := strings.Replace(string(dat), "_USERNAME_", user, -1)
	body = strings.Replace(body, "_TOKEN_", password, -1)
	body = strings.Replace(body, "_NODEID_", nodeid, -1)
	body = strings.Replace(body, "_IP_", ip, -1)
	body = strings.Replace(body, "_PORT_", port, -1)
	body = strings.Replace(body, "_TIME_", myTime, -1)
	body = strings.Replace(body, "_DATA_", encoded, -1)

	// not set Content-Type
	req.Body = strings.NewReader(string(body))
	resp, err := req.Post(target)

	if err != nil {
		fmt.Println("Error http post:", err.Error())
	} else {
		fmt.Println("Http Reponse", resp.Status)
	}

}

func initLogger() *zap.Logger {

	rawJSON := []byte(`{
	  "level": "debug",
	  "encoding": "json",
	  "outputPaths": ["stdout", "/var/log/medpot/medpot.log"],
	  "errorOutputPaths": ["stderr"],
	  "encoderConfig": {
	    "messageKey": "message",
	    "levelKey": "level",
	    "levelEncoder": "lowercase"
	  }
	}`)

	var cfg zap.Config
	if err := json.Unmarshal(rawJSON, &cfg); err != nil {
		panic(err)
	}
	logger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	return logger
}

func main() {

	fmt.Print("Starting Medpot at ")
	currentTime := time.Now().UTC().Format(time.RFC3339)
	fmt.Println(currentTime)

	target, user, password, nodeid := readConfig()

	logger := initLogger()

	l, err := net.Listen(CONN_TYPE, ":"+CONN_PORT)

	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()

	fmt.Println("Listening on " + CONN_HOST + ":" + CONN_PORT)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}

		// Handle connections in a new goroutine.
		go handleRequest(conn, logger, target, user, password, nodeid)
	}
}

/*
	reads file from both possible locations (first repo location, second location from docker install
*/
func readFile(name string) []byte {

	b1 := make([]byte, 4)

	dat, err := ioutil.ReadFile("./template/" + name)
	if err == nil {
		return dat
	}

	dat, err = ioutil.ReadFile("/data/medpot/" + name)
	if err == nil {
		return dat

	}

	return b1

}

func handleClientRequest(conn net.Conn, buf []byte, reqLen int) {

	dat := readFile("dummyerror.xml")

	// copy to a real buffer
	bufTarget := make([]byte, reqLen)
	copy(bufTarget, buf)

	s := string(buf)
	if strings.Contains(s, "MSH") {

		if strings.Index(s, "MSH|") == 0 {

			dat = readFile("dummyok.xml")

		}

	}

	// Send a response back to person contacting us.
	conn.Write(dat)

}

// Handles incoming requests.
func handleRequest(conn net.Conn, logger *zap.Logger, target string, user string, password string, nodeid string) {
	// Make a buffer to hold incoming data.

	buf := make([]byte, 1024*1024)
	counter := 0

	for {

		timeoutDuration := 3 * time.Second
		conn.SetReadDeadline(time.Now().Add(timeoutDuration))

		// Read the incoming connection into the buffer.
		reqLen, err := conn.Read(buf)
		if err != nil {

			if err.Error() != "EOF" {
				fmt.Println("Error reading:", err.Error())
			}
			conn.Close()
			break
		} else {

			remote := fmt.Sprintf("%s", conn.RemoteAddr())
			ip, port, _ := net.SplitHostPort(remote)
			currentTime := time.Now().UTC().Format(time.RFC3339)
			fmt.Print(currentTime)
			myTime := currentTime

			fmt.Print(": Connecting from ip ", ip)
			fmt.Println(" and port ", port)

			handleClientRequest(conn, buf, reqLen)

			// copy to a real buffer
			bufTarget := make([]byte, reqLen)
			copy(bufTarget, buf)

			spew.Dump(bufTarget)

			encoded := base64.StdEncoding.EncodeToString([]byte(bufTarget))

			logger.Info("Connection found",
				// Structured context as strongly typed Field values.
				zap.String("timestamp", myTime),
				zap.String("src_port", port),
				zap.String("src_ip", ip),
				zap.String("data", encoded),
			)

			// if configured, send bacxk data to PEBA / DTAG T_pot homebase
			post(target, user, password, nodeid, myTime, port, ip, encoded)

		}

		counter = counter + 1
		fmt.Println("Increase counter ...")
		if counter == 3 {
			fmt.Println(("Maximum loop counter reached...."))
			conn.Close()
		}
	}

}
