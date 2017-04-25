package main

import (
	"fmt"
	"net"
	"os"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/go-redis/redis"
	"github.com/satori/go.uuid"
	"gopkg.in/igm/sockjs-go.v2/sockjs"
	"log"
	"net/http"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "8086"
	CONN_TYPE = "tcp"
)

var redisClient *redis.Client

type InvalidAction struct {
	Success bool `json:"success"`
	Reason string `json:"reason"`
	Code int32 `json:"code"`
}
type RegNS_OK struct {
	Success bool `json:"success"`
	SecretKey string `json:"secretKey"`
}
type Success struct {
	Success bool `json:"success"`
}

func runWS_Server()  {
	fmt.Println("Run WS server")
	handler := sockjs.NewHandler("/socket", sockjs.DefaultOptions, handlerWS)
	log.Fatal(http.ListenAndServe(":8080", handler))
}

func runNET_Server()  {
	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		os.Exit(1)
	}
	// Close the listener when the application closes.
	defer l.Close()
	fmt.Println("Run NET server on " + CONN_HOST + ":" + CONN_PORT)
	for {
		// Listen for an incoming connection.
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("Error accepting: ", err.Error())
			os.Exit(1)
		}
		// Handle connections in a new goroutine.
		go handleRequest(conn)
	}
}

func main() {
	// =======================
	// Connect to redis
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "dizardKom@cherS", // no password set
		DB:       0,  // use default DB
	})
	go runWS_Server()
	runNET_Server()
}

type WS_Server struct {
	connections map[string]sockjs.Session
}
func NewWS_Server() *WS_Server {
	return &WS_Server{
		connections: make(map[string] sockjs.Session),
	}
}

var connections map[string]sockjs.Session

func handlerWS(session sockjs.Session) {
	fmt.Println("new connection to WS server")
	fmt.Printf("%s %T", session.ID(), session)
	WSMap := NewWS_Server()
	WSMap.connections[session.ID()] = session

	for {
		if msg, err := session.Recv(); err == nil {
			session.Send(msg)
			continue
		}
		break
	}
}


func convertByteToInt(in []byte) int32 {
	return  (int32(in[0]) << 24 | int32(in[1]) << 16 | int32(in[2]) << 8 | int32(in[3]))
}

// Handles incoming requests.
func handleRequest(conn net.Conn) {
	fmt.Println("new connection")
	for{
		var lengthData int32
		var err error
		// Make a buffer to hold incoming data.
		var readbuf = make([]byte, 1024)

		// Read the incoming connection into the buffer.
		reqLen, err := conn.Read(readbuf)
		if (reqLen>0) {}
		if err != nil {
			fmt.Println("Error reading:", err.Error())
			conn.Close()
			return
		}
		// Send a response back to person contacting us.
		fmt.Printf("length: %d \n", reqLen)

		b := bytes.NewBuffer(readbuf)
		// Считываем длину сообщения
		binary.Read(b, binary.LittleEndian, &lengthData)
		if lengthData < 1 {
			conn.Close();
			return  ;
		}
		fmt.Printf("lengthData: %d \n", lengthData)

		data := readbuf[4 : 4+lengthData]
		fmt.Println(string(data))

		if (!handlerCommand(data, conn)) {
			conn.Close()
			break
		}
	}
}
// Обрабочик команд TCP сервера
func handlerCommand(data []byte, conn net.Conn) bool {
	var anything interface{}
	err := json.Unmarshal(data, &anything)
	if ( err!=nil ) {
		return false
	}
	fmt.Printf("%v", anything)

	dataMapStr := anything.(map[string]interface{})
	if (dataMapStr["action"]=="auth") {
		fmt.Println("auth \n")
		var siteId string
		var secretKey string

		if val, ok := dataMapStr["sKey"].(string); ok {
			secretKey = val
		}else{
			return false
		}
		if val, ok := dataMapStr["name"].(string); ok {
			siteId = val
		}else{
			return false
		}

		has, err := redisClient.Exists("LaWS_Server:name_spaces:" + siteId).Result()
		if (has==0) {
			sendData(InvalidAction{Success:false, Reason:"Name space not found", Code:404}, conn)
			return true
		}
		val, err := redisClient.Get("LaWS_Server:name_spaces:" + siteId).Result()
		if err != nil {
			fmt.Println("redis Error",err)
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return true
		}
		if (val==secretKey) {
			sendData(Success{Success:true}, conn)
			return true
		}else{
			sendData(InvalidAction{Success:false, Reason:"Invalid sKey", Code:305}, conn)
			return true
		}
	}
	if (dataMapStr["action"]=="registerNameSpace") {
		fmt.Println("registerNameSpace")
		var name string
		var key string
		if val, ok := dataMapStr["name"].(string); ok {
			name = val
		}else{
			sendData(InvalidAction{Success:false, Reason:"Need name", Code:300}, conn)
			return false
		}
		if val, ok := dataMapStr["key"].(string); ok {
			key = val
		}else{
			sendData(InvalidAction{Success:false, Reason:"Need key", Code:300}, conn)
			return false
		}
		if (key!="asd82nvakadfs") {
			sendData(InvalidAction{Success:false, Reason:"Invalid key", Code:306}, conn)
			return false;
		}
		has, err := redisClient.Exists("LaWS_Server:name_spaces:" + name).Result()
		if (err != nil) {
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return false
		}
		if (has==1) {
			sendData(InvalidAction{Success:false, Reason:"Name space is busy", Code:309}, conn)
			return false
		}
		secretKey := uuid.NewV4().String()
		err = redisClient.Set("LaWS_Server:name_spaces:"+name, secretKey, 0).Err()
		if err != nil {
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return false
		}
		sendData(RegNS_OK{Success:true, SecretKey:secretKey}, conn)
		return true
	}
	if (dataMapStr["action"]=="emit") {

	}
	if (dataMapStr["action"]=="set") {

	}
	if (dataMapStr["action"]=="get") {

	}
	if (dataMapStr["action"]=="channelInfo") {

	}
	if (dataMapStr["action"]=="subscribe") {

	}
	if (dataMapStr["action"]=="unsubscribe") {

	}

	sendData(InvalidAction{Success:false, Reason:"Invalid action...", Code:312}, conn)
	return true
}

func sendData(data interface{}, conn net.Conn) bool {
	dataBytes, err := json.Marshal(data)
	if ( err!=nil ) {
		return false
	}
	sz := len(dataBytes)
	fmt.Printf("len %d \n", sz)
	buffer := bytes.NewBuffer(make([]byte, 0, 5+sz))
	binary.Write(buffer, binary.LittleEndian, int32(sz))
	buffer.Write(dataBytes)
	binary.Write(buffer, binary.LittleEndian, byte(0))
	binary.Write(buffer, binary.LittleEndian, byte(0))
	conn.Write(buffer.Bytes())
	fmt.Printf("answer %s \n", buffer.String())
	return true
}
