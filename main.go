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
	"github.com/dgrijalva/jwt-go"
	"strconv"
)

const (
	CONN_HOST = "localhost"
	CONN_PORT = "8086"
	CONN_TYPE = "tcp"
)

var redisClient *redis.Client

func ItoStr(v interface{}) string {
	switch vv := v.(type) {
	case string:
		return vv
	case int:
		return string(vv)
	case float64:
		return strconv.FormatFloat(vv, 'f', 0, 64)
	default:
		return ""
	}
}
func ItoBool(v interface{}) bool {
	switch vv := v.(type) {
	case string:
		return vv!=""
	case int:
		return vv>0
	case float64:
		return vv>0
	default:
		return false
	}
}
func ItoInt(v interface{}) int {
	switch vv := v.(type) {
	case string:
		i, _ := strconv.ParseInt(vv, 10, 64)
		return int(i)
	case int:
		return vv
	case float64:
		return int(vv)
	default:
		return 0
	}
}



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
type IWSConn struct {
	conn sockjs.Session
	channels []string
	siteId string
	userId string
}
type IStore struct{}
func (c *IStore) save(siteId string, channel string, message string, userid string, ttl int) bool {
	key := "LaWS_Server:store:"+siteId+":"+channel
	if userid!="" {
		key = "LaWS_Server:store:"+siteId+":"+userid+":"+channel
	}
	fmt.Printf("save: %s - %s\n", key, message)
	redisClient.Set(key, message, 0).Err()
	return true
}
func (c *IStore) load(siteId string, channel string, userid string) string {
	key := "LaWS_Server:store:"+siteId+":"+channel
	if userid!="" {
		key = "LaWS_Server:store:"+siteId+":"+userid+":"+channel
	}
	//fmt.Printf("load: %s \n", key)
	data, err := redisClient.Get(key).Result()
	if err==nil {
		return data
	}
	return ""
}

type WS_Server struct {
	connections map[string]IWSConn
	// siteId userId sessionid = session
	NS_USER map[string]map[string]map[string]sockjs.Session
	// siteId  channel  userId
	PRIVATE map[string]map[string]map[string]int
	// siteId channelName userId  sessionid = session
	NS_CHANNEL_USER map[string]map[string]map[string]map[string]sockjs.Session
	Store IStore
}
func (c *WS_Server) emit(siteId string, channel string, message string, userid string) bool {
	if _, ok := WSSrv.NS_CHANNEL_USER[siteId]; !ok { return true }
	if _, ok := WSSrv.NS_CHANNEL_USER[siteId][channel]; !ok { return true }
	if channel[0:1]=="@" {
		// Пользовательский канал
		if _, ok := WSSrv.NS_CHANNEL_USER[siteId][channel][userid]; !ok { return true }
		for kS := range WSSrv.NS_USER[siteId][userid] {
			WSSrv.NS_USER[siteId][userid][kS].Send(message)
		}
		return true
	}
	for kU := range WSSrv.NS_CHANNEL_USER[siteId][channel] {
		for kS := range WSSrv.NS_USER[siteId][kU] {
			WSSrv.NS_USER[siteId][kU][kS].Send(message)
		}
	}
	return true
}
func (c *WS_Server) get(siteId string, channel string, userid string) []byte {
	return []byte(c.Store.load(siteId, channel, userid))
}
func (c *WS_Server) set(siteId string, channel string, message string, userid string, emit bool, ttl int) bool {
	c.Store.save(siteId, channel, message, userid, ttl)
	if emit {c.emit(siteId, channel, message, userid)}
	return true
}
func (c *WS_Server) subscribe(siteId string, channel string, userid string) bool {
	mm, ok := c.PRIVATE[siteId]
	if  !ok {mm = make(map[string]map[string]int)}
	c.PRIVATE[siteId] = mm
	mmm, ok := c.PRIVATE[siteId][channel]
	if  !ok{mmm = make(map[string]int)}
	c.PRIVATE[siteId][channel] = mmm
	c.PRIVATE[siteId][channel][userid] = 1
	return true
}
func (c *WS_Server) unSubscribe(siteId string, channel string, userid string) bool {
	_, ok := c.PRIVATE[siteId]
	if  !ok {return true}
	_, ok = c.PRIVATE[siteId][channel]
	if  !ok {return true}
	_, ok = c.PRIVATE[siteId][channel][userid]
	if  !ok {return true}

	// Чистим лишнее
	delete(c.PRIVATE[siteId][channel], userid)
	if len(c.PRIVATE[siteId][channel])==0 {
		delete(c.PRIVATE[siteId], channel)
	}
	if len(c.PRIVATE[siteId])==0 {
		delete(c.PRIVATE, siteId)
	}
	return true
}
func (c *WS_Server) channelInfo(siteId string, channel string) []byte {
	return []byte("")
}




var WSSrv *WS_Server

func runWS_Server()  {
	fmt.Println("Run WS server")
	WSSrv = &WS_Server{
		connections: make(map[string]IWSConn),
		NS_USER: make(map[string]map[string]map[string]sockjs.Session),
		PRIVATE: make(map[string]map[string]map[string]int),
		NS_CHANNEL_USER: make(map[string]map[string]map[string]map[string]sockjs.Session),
		Store: IStore{},
	}
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



var connections map[string]sockjs.Session

type jAuth struct {
	Event string `json:"event"`
	Data map[string]string `json:"data"`
}
type jSubscribe struct {
	Event string `json:"event"`
	Data string `json:"data"`
}
type aMess struct {
	Event string `json:"event"`
	Data *json.RawMessage `json:"data"`
}

type AuthOk struct {
	Event string `json:"event"`
	Data map[string]string `json:"data"`
}

type JQuery struct {
	Action  string `json:"action"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
	SKey    string `json:"sKey"`
	Key     string `json:"key"`
	Data    map[string]interface{} `json:"data"`
	Params  map[string]interface{} `json:"params"`

}

func handlerWS(session sockjs.Session) {
	//fmt.Println("new connection to WS server")

	var WSConn = IWSConn{conn: session, siteId:"", userId:""}
	WSSrv.connections[session.ID()] = WSConn

	// Слушаем что нам шлет сокет
	for {
		if msg, err := session.Recv(); err == nil {
			//fmt.Printf("new message %s \n", msg)

			var mapMess interface{}
			err := json.Unmarshal([]byte(msg), &mapMess)
			if err!=nil {continue}
			mapMessStr := mapMess.(map[string]interface{})
			//fmt.Printf("%v\n", mapMess)

			// =========================
			// WS Auth
			if mapMessStr["event"]=="auth" {
				var siteId string
				var authStr string
				var data map[string] interface{}

				if val, ok := mapMessStr["data"]; ok {
					//fmt.Printf("%v\n", val)
					data = val.(map[string] interface{})
				}else{
					session.Close(3002, "not found data - param")
					continue
				}

				if val, ok :=data["i"]; ok {
					siteId = val.(string)
				}else{
					session.Close(3002, "not found i - param, site id")
					continue
				}
				if val, ok := data["s"]; ok {
					authStr = val.(string)
				}else{
					session.Close(3002, "not found s - param, auth string")
					continue
				}

				nSpace, err := redisClient.Get("LaWS_Server:name_spaces:" + siteId).Result()
				if err != nil {
					//fmt.Printf("ns: %s \n",siteId)
					//fmt.Printf("erro %v \n",err)
					session.Close(3050, "error strore")
					continue
				}
				if (nSpace=="") {
					session.Close(3404, "site id not registered")
					continue
				}
				//fmt.Printf("parse JWT %s \nsecret: %s \n", authStr, nSpace)
				// Парсим токен
				token, err := jwt.Parse(authStr, func(token *jwt.Token) (interface{}, error) {
					return []byte(nSpace), nil
				})
				//fmt.Printf("JWT %v \n", err)
				if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
					//fmt.Printf("JWT %v \n", token.Claims)
					data := make(map[string] string)
					data["cid"] = session.ID()

					dataBytes, err := json.Marshal(AuthOk{Event:"auth", Data:data})
					if err==nil {
						userId := claims["i"].(string)
						//fmt.Printf("siteId %s userId %s session %s %T\n", siteId, userId, session.ID(), session)

						mm, ok := WSSrv.NS_USER[siteId]
						if  !ok {mm = make(map[string]map[string]sockjs.Session)}
						WSSrv.NS_USER[siteId] = mm
						mmm, ok := WSSrv.NS_USER[siteId][userId]
						if  !ok{mmm = make(map[string]sockjs.Session)}
						WSSrv.NS_USER[siteId][userId] = mmm
						WSSrv.NS_USER[siteId][userId][session.ID()] = session

						WSConn.siteId = siteId
						WSConn.userId = userId
						session.Send(string(dataBytes))
					}else{
						session.Close(3404, "Invalid token")
						continue
					}
				} else {
					session.Close(3404, "Invalid token")
					continue
				}
			}
			// =========================
			// WS subscribe
			if mapMessStr["event"]=="subscribe" {
				if (WSConn.siteId=="" || WSConn.userId=="") {
					WSConn.conn.Send("need auth")
					continue
				}

				var channelName string
				if val, ok := mapMessStr["data"]; ok {
					channelName = val.(string)
				}else{
					continue
				}
				//fmt.Println(channelName)
				WSConn.channels = append(WSConn.channels, channelName)

				mm, ok := WSSrv.NS_CHANNEL_USER[WSConn.siteId]
				if  !ok {mm = make(map[string]map[string]map[string]sockjs.Session)}
				WSSrv.NS_CHANNEL_USER[WSConn.siteId] = mm

				mmm, ok := WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName]
				if  !ok{mmm = make(map[string]map[string]sockjs.Session)}
				WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName] = mmm

				mmmm, ok := WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName][WSConn.userId]
				if  !ok{mmmm = make(map[string]sockjs.Session)}
				WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName][WSConn.userId] = mmmm

				WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName][WSConn.userId][session.ID()] = session


				if channelName[0:1]=="@" {
					data := WSSrv.Store.load(WSConn.siteId, channelName, WSConn.userId)
					if data!="" {WSConn.conn.Send(string(data))}
				}else if channelName[0:1]!="#" {
					//fmt.Printf("=========== %s \n", channelName)
					data := WSSrv.Store.load(WSConn.siteId, channelName, "")
					if data=="" {continue}

					dataB := []byte(data)
					out, err := json.Marshal(aMess{Event:channelName, Data:(*json.RawMessage)(&dataB)})
					if err==nil {WSConn.conn.Send(string(out))}
				}
			}
			continue
		}
		break
	}

	// ===============================================
	// =========== connection closed =================
	for _, channelName := range WSConn.channels {
		//fmt.Printf("channel %s \n", channelName)
		delete(WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName][WSConn.userId], WSConn.siteId)

		// Очищаем пустой map
		if len(WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName][WSConn.userId]) == 0 {
			delete(WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName], WSConn.userId)
		}
		if len(WSSrv.NS_CHANNEL_USER[WSConn.siteId][channelName]) == 0 {
			delete(WSSrv.NS_CHANNEL_USER[WSConn.siteId], channelName)
		}
		if len(WSSrv.NS_CHANNEL_USER[WSConn.siteId]) == 0 {
			delete(WSSrv.NS_CHANNEL_USER, WSConn.siteId)
		}
	}
	delete(WSSrv.connections, WSConn.conn.ID())
}

//func convertByteToInt(in []byte) int32 {
//	return  (int32(in[0]) << 24 | int32(in[1]) << 16 | int32(in[2]) << 8 | int32(in[3]))
//}

// Handles incoming requests.
func handleRequest(conn net.Conn) {
	fmt.Println("new connection")
	var CSiteId string
	var lengthData int32
	var err error
	// Make a buffer to hold incoming data.
	var ok bool
	var newSiteId string

	for{
		readbuf := make([]byte, 4)
		_, err = conn.Read(readbuf)
		if err != nil {
			fmt.Println("Error reading:", err.Error())
			conn.Close()
			return
		}
		b := bytes.NewBuffer(readbuf)
		binary.Read(b, binary.LittleEndian, &lengthData)
		fmt.Printf("lengthData: %d \n", lengthData)
		if lengthData < 1 {
			conn.Close()
			return
		}
		readbuf = make([]byte, lengthData+1)
		readed, err := conn.Read(readbuf)
		if err != nil {
			//fmt.Println("Error reading:", err.Error())
			conn.Close()
			return
		}
		fmt.Printf("readed %d \n", readed)

		ok, newSiteId = handlerCommand(readbuf[0 : lengthData], conn, CSiteId)

		if !ok {conn.Close(); break}
		if newSiteId!="" {CSiteId = newSiteId}
	}
}
// Обрабочик команд TCP сервера
func handlerCommand(data []byte, conn net.Conn, CSiteId string) (bool, string) {
	var request JQuery
	err := json.Unmarshal(data, &request)
	if  err!=nil  {
		fmt.Println("Error", err)
		return false, ""
	}

	// TODO check auth
	fmt.Printf("===== %v\n", request)
	if request.Action=="auth" {
		if request.SKey =="" {return false, ""}
		if request.Name=="" {return false, ""}
		fmt.Println("auth \n")
		has, err := redisClient.Exists("LaWS_Server:name_spaces:" + request.Name).Result()
		if has==0 || err !=nil {
			sendData(InvalidAction{Success:false, Reason:"Name space not found", Code:404}, conn)
			return true, ""
		}
		val, err := redisClient.Get("LaWS_Server:name_spaces:" + request.Name).Result()
		if err != nil {
			//fmt.Println("redis Error",err)
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return true, ""
		}
		if val==request.SKey {
			sendData(Success{Success:true}, conn)
			return true, request.Name
		}
		sendData(InvalidAction{Success:false, Reason:"Invalid sKey", Code:305}, conn)
		return true, ""
	}
	if request.Action=="registerNameSpace" {
		if request.Name=="" {
			sendData(InvalidAction{Success:false, Reason:"Need name", Code:300}, conn)
			return false, ""
		}
		if request.Key=="" {
			sendData(InvalidAction{Success:false, Reason:"Need key", Code:300}, conn)
			return false, ""
		}
		if request.Key!="asd82nvakadfs" {
			sendData(InvalidAction{Success:false, Reason:"Invalid key", Code:306}, conn)
			return false, ""
		}
		//fmt.Println("registerNameSpace")

		has, err := redisClient.Exists("LaWS_Server:name_spaces:" + request.Name).Result()
		if err != nil {
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return false, ""
		}
		if has==1 {
			sendData(InvalidAction{Success:false, Reason:"Name space is busy", Code:309}, conn)
			return false, ""
		}
		secretKey := uuid.NewV4().String()
		err = redisClient.Set("LaWS_Server:name_spaces:"+request.Name, secretKey, 0).Err()
		if err != nil {
			sendData(InvalidAction{Success:false, Reason:"Error store, try latter...", Code:302}, conn)
			return false, ""
		}
		sendData(RegNS_OK{Success:true, SecretKey:secretKey}, conn)
		return true, ""
	}

	// Check auth
	if CSiteId=="" {
		fmt.Println("Need auth")
		sendData(InvalidAction{Success:false, Reason:"Need auth", Code:311}, conn)
		return true, ""
	}
	if request.Action=="emit" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		var userId string
		if v, ok := request.Params["userId"]; ok {userId = ItoStr(v)}

		out, _ := json.Marshal(request.Data)
		WSSrv.emit(CSiteId, request.Channel, string(out), userId)
		sendData(Success{Success:true}, conn)
		return true, ""
	}
	if request.Action=="set" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		var userId string
		var emit bool
		var ttl int
		if v, ok := request.Params["userId"]; ok {userId = ItoStr(v)}
		if v, ok := request.Params["ttl"]; ok {ttl = ItoInt(v)}

		out, err := json.Marshal(request.Data)
		fmt.Printf("error %T", err)
		WSSrv.set(CSiteId, request.Channel, string(out), userId, emit, ttl)
		sendData(Success{Success:true}, conn)
		return true, ""
	}
	if request.Action=="get" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		var userId string
		if v, ok := request.Params["userId"]; ok {userId = ItoStr(v)}
		sendDataBytes(WSSrv.get(CSiteId, request.Channel, userId), conn)
		return true, ""
	}
	if request.Action=="subscribe" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		var userId string
		if v, ok := request.Params["userId"]; ok {
			userId = ItoStr(v)
		}else{
			sendData(InvalidAction{Success:false, Reason:"Need userid", Code:300}, conn)
			return true, ""
		}

		WSSrv.subscribe(CSiteId, request.Channel, userId)
		sendData(Success{Success:true}, conn)
		return true, ""
	}
	if request.Action=="unsubscribe" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		var userId string
		if v, ok := request.Params["userId"]; ok {
			userId = ItoStr(v)
		}else{
			sendData(InvalidAction{Success:false, Reason:"Need userid", Code:300}, conn)
			return true, ""
		}

		WSSrv.unSubscribe(CSiteId, request.Channel, userId)
		sendData(Success{Success:true}, conn)
		return true, ""
	}
	if request.Action=="channelInfo" {
		if request.Channel=="" {
			sendData(InvalidAction{Success:false, Reason:"Need channel", Code:300}, conn)
			return true, ""
		}
		sendDataBytes(WSSrv.channelInfo(CSiteId, request.Channel), conn)
		return true, ""
	}

	sendData(InvalidAction{Success:false, Reason:"Invalid action...", Code:312}, conn)
	return true, ""
}

func sendDataBytes(dataBytes []byte, conn net.Conn) bool {
	sz := len(dataBytes)
	//fmt.Printf("len %d \n", sz)
	buffer := bytes.NewBuffer(make([]byte, 0, 5+sz))
	binary.Write(buffer, binary.LittleEndian, int32(sz))
	buffer.Write(dataBytes)
	binary.Write(buffer, binary.LittleEndian, byte(0))
	//binary.Write(buffer, binary.LittleEndian, byte(0))
	conn.Write(buffer.Bytes())
	//fmt.Printf("answer %s \n", buffer.String())
	return true
}
func sendData(data interface{}, conn net.Conn) bool {
	dataBytes, err := json.Marshal(data)
	if  err!=nil  {return false}
	return sendDataBytes(dataBytes, conn)
}
