package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/gorilla/websocket"
)

var clients = make(map[*websocket.Conn]bool)      // connected clients
var broadcast = make(chan map[string]interface{}) // broadcast channel
var dhcpdStatus = make(map[string]map[string]interface{})
var dhcpds []string = strings.Split(os.Getenv("DHCPDS"), ",")

// Configure the upgrader
var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	port := flag.String("port", "8000", "Specify port to bind")
	if flag.Parse(); *verbose {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if len(dhcpds) == 1 && dhcpds[0] == "" {
		logrus.Fatalf("Environment DHCPDS not set (properly).")
	}

	// Create a simple file server
	fs := http.FileServer(http.Dir("./public"))
	http.Handle("/", fs)

	// Configure websocket route
	http.HandleFunc("/ws", handleConnections)

	// Start listening for incoming messages
	go handleMessages()

	// Start the server and log any errors
	logrus.Infof("http server started on :%s", *port)
	err := http.ListenAndServe(":"+*port, nil)
	if err != nil {
		logrus.Fatalf("Failed to start server: %s", err.Error())
	}
}

func sendRequestToDHCPD(u *url.URL, rs chan<- map[string]interface{}) {
	dhcpdStateData := make(map[string]interface{})
	dhcpdStateData["server"] = u.Host
	logrus.Debugf("Get data for: %s", u.Host)
	res, err := http.Get(u.String())
	if err != nil || res.StatusCode != 200 {
		logrus.Debugf("Failed to get state from dhcpcd: %s", err)
		dhcpdStateData["status"] = "offline"
		rs <- dhcpdStateData
		return
	}
	defer res.Body.Close()

	str, _ := ioutil.ReadAll(res.Body)
	json.Unmarshal([]byte(str), &dhcpdStateData)
	dhcpdStateData["status"] = "online"
	rs <- dhcpdStateData
}

func getData(urls []string) {
	res := make(chan map[string]interface{})
	for _, urlPath := range urls {
		u, _ := url.Parse(urlPath)

		go sendRequestToDHCPD(u, res)
	}

	for range urls {
		// Send the newly received message to the broadcast channel
		broadcast <- <-res
	}

}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	// Upgrade initial GET request to a websocket
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logrus.Errorf("Error upgrading connection: %s", err.Error())
	}
	// Make sure we close the connection when the function returns
	defer ws.Close()

	// Register our new client
	clients[ws] = true

	for {
		// Get data from dhcpd monitor
		<-time.After(3 * time.Second)
		getData(dhcpds)
	}
}

// ByServer implements sort.Interface for []Person based on
// the Age field.
type ByServer []map[string]interface{}

func (a ByServer) Len() int      { return len(a) }
func (a ByServer) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByServer) Less(i, j int) bool {
	ip1, _, _ := net.ParseCIDR(a[i]["server"].(string) + "/24")
	ip2, _, _ := net.ParseCIDR(a[j]["server"].(string) + "/24")
	return bytes.Compare(ip1, ip2) < 0
}

func makeSliceFromMap(myMap map[string]map[string]interface{}) []map[string]interface{} {
	mySlice := make([]map[string]interface{}, 0, len(myMap))
	for _, obj := range myMap {
		mySlice = append(mySlice, obj)
	}
	return mySlice
}

func handleMessages() {
	for {
		// Grab the next message from the broadcast channel
		msg := <-broadcast
		// Send it out to every client that is currently connected
		dhcpdStatus[msg["server"].(string)] = msg
		for client := range clients {
			allStates := makeSliceFromMap(dhcpdStatus)
			sort.Sort(ByServer(allStates))
			err := client.WriteJSON(allStates)
			if err != nil {
				logrus.Errorf("error: %s", err.Error())
				client.Close()
				delete(clients, client)
			}
		}
	}
}
