package main

import (
	"fmt"
	"net"
	"os"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"github.com/go-redis/redis"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "8085"
	CONN_TYPE = "tcp"
)

var redisClient *redis.Client

func main() {
	// Connect to redis
	redisClient = redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "dizardKom@cherS", // no password set
		DB:       0,  // use default DB
	})


	// NET Server
	l, err := net.Listen(CONN_TYPE, CONN_HOST+":"+CONN_PORT)
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
		go handleRequest(conn)
	}
}

func convertByteToInt(in []byte) int32 {
	return  (int32(in[0]) << 24 | int32(in[1]) << 16 | int32(in[2]) << 8 | int32(in[3]))
}

// Handles incoming requests.
func handleRequest(conn net.Conn) {
	fmt.Println("new connection")

	var lengthData int32;
	var err error;
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

	if (!handlerCommand(data)) {
		conn.Close()
	}

	//bufMess := bytes.NewBuffer(buf)
	//lengthData, err = binary.ReadUvarint(bufMess)



	// Close the connection when you're done with it.

}


func handlerCommand(data []byte) bool {
	var anything interface{}
	err := json.Unmarshal(data, &anything)
	if ( err!=nil ) {
		return false
	}
	fmt.Printf("%v", anything)

	dataMapStr := anything.(map[string]interface{})
	if (dataMapStr["action"]=="auth") {
		fmt.Println("auth")
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
		fmt.Println("siteId: ", siteId)
		val, err := redisClient.Get("LaWS_Server:name_spaces:" + siteId).Result()
		if err != nil {
			fmt.Println(err)
			//return sendData(sock, {'success' : false, reason : 'Error store, try latter...', code: 302});
		}
		if (val==secretKey) {
			fmt.Println("site found \n")
		}
	}
	return true
}
