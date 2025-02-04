package douyin

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/errors"
	dyproto "github.com/zboyco/douyin-live-go/protobuf"
	"google.golang.org/protobuf/proto"
)

type Room struct {
	Url       string // 房间地址
	Ttwid     string
	RoomStore string
	RoomId    string
	RoomTitle string
	wsConnect *websocket.Conn

	fns []func(message any)
}

func NewRoom(roomID string, handles ...func(message any)) (*Room, error) {
	h := map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36",
		"cookie":     "odin_tt=5358a98f55c32065cdfd667f8e87f40fc846f4852cfe8e29ec0cee8bc5b89672315a9b453c8d8cf8f72988bb455cdf75f7ecd1cf70e20776178974236d95a03bbb2d51f2ef8c511e6c74efdcb0b65b20;  __ac_nonce=0638733a400869171be51;",
	}
	roomUrl := fmt.Sprintf("https://live.douyin.com/%s", roomID)
	req, err := http.NewRequest("GET", roomUrl, nil)
	if err != nil {
		log.Printf("NewRequest failed, error: %v", err)
		return nil, err
	}
	for k, v := range h {
		req.Header.Set(k, v)
	}
	client := &http.Client{
		Timeout: time.Second * 10,
	}
	log.Println("[系统] 开始获取房间信息...")
	res, err := client.Do(req)
	if err != nil {
		log.Printf("[系统] 获取房间信息失败, 错误信息: %v", err)
		return nil, err
	}
	defer res.Body.Close()
	data := res.Cookies()
	var ttwid string
	for _, c := range data {
		if c.Name == "ttwid" {
			ttwid = c.Value
			break
		}
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Printf("[系统] 读取房间信息失败, error: %v", err)
		return nil, err
	}
	log.Println("[系统] 获取房间信息成功")
	resText := string(body)
	re := regexp.MustCompile(`roomId\\":\\"(\d+)\\"`)
	match := re.FindStringSubmatch(resText)
	if match == nil || len(match) < 2 {
		err = errors.New("直播间ID获取失败, No match found")
		slog.Error(err.Error())
		return nil, err
	}
	liveRoomId := match[1]
	return &Room{
		Url:    roomUrl,
		Ttwid:  ttwid,
		RoomId: liveRoomId,
		fns:    handles,
	}, nil
}

func (r *Room) Connect() error {
	wsUrl := "wss://webcast3-ws-web-lq.douyin.com/webcast/im/push/v2/?app_name=douyin_web&version_code=180800&webcast_sdk_version=1.3.0&update_version_code=1.3.0&compress=gzip&internal_ext=internal_src:dim|wss_push_room_id:{ROOM_ID}|wss_push_did:{ROOM_ID}|dim_log_id:{DATATIME}1A160A7BAA76660E13ED|fetch_time:{TIMESTAMP}|seq:1|wss_info:0-{TIMESTAMP}-0-0|wrds_kvs:WebcastRoomStatsMessage-{TIMESTAMP}146024_WebcastRoomRankMessage-{TIMESTAMP}895075_AudienceGiftSyncData-{TIMESTAMP}317696_HighlightContainerSyncData-2&cursor=t-{TIMESTAMP}_r-1_d-1_u-1_h-1&host=https://live.douyin.com&aid=6383&live_id=1&did_rule=3&debug=false&endpoint=live_pc&support_wrds=1&im_path=/webcast/im/fetch/&user_unique_id={ROOM_ID}&device_platform=web&cookie_enabled=true&screen_width=1440&screen_height=900&browser_language=zh&browser_platform=MacIntel&browser_name=Mozilla&browser_version=5.0%20(Macintosh;%20Intel%20Mac%20OS%20X%2010_15_7)%20AppleWebKit/537.36%20(KHTML,%20like%20Gecko)%20Chrome/118.0.0.0%20Safari/537.36&browser_online=true&tz_name=Asia/Shanghai&identity=audience&room_id={ROOM_ID}&heartbeatDuration=0&signature=00000000"

	currentTime := time.Now()
	wsUrl = strings.Replace(wsUrl, "{ROOM_ID}", r.RoomId, -1)
	wsUrl = strings.Replace(wsUrl, "{TIMESTAMP}", fmt.Sprintf("%d", currentTime.UnixNano()), -1)
	wsUrl = strings.Replace(wsUrl, "{DATATIME}", currentTime.Format("20060102150405"), -1)

	h := http.Header{}
	h.Set("cookie", "ttwid="+r.Ttwid)
	h.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/108.0.0.0 Safari/537.36")

	log.Println("[系统] 开始连接直播数据...")
	wsConn, wsResp, err := websocket.DefaultDialer.Dial(wsUrl, h)
	if err != nil {
		log.Println("[系统] 连接失败, 错误信息:", err)
		return err
	}
	log.Printf("[系统] 连接成功,状态码:%d", wsResp.StatusCode)
	r.wsConnect = wsConn
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		r.read()
	}()
	go func() {
		defer wg.Done()
		r.send()
	}()
	wg.Wait()
	return nil
}

func (r *Room) read() {
	for {
		_, data, err := r.wsConnect.ReadMessage()
		if err != nil {
			panic(err.Error())
		}
		var msgPack dyproto.PushFrame
		_ = proto.Unmarshal(data, &msgPack)
		// log.Println(msgPack.LogId)
		decompressed, _ := degzip(msgPack.Payload)
		var payloadPackage dyproto.Response
		_ = proto.Unmarshal(decompressed, &payloadPackage)
		if payloadPackage.NeedAck {
			r.sendAck(msgPack.LogId, payloadPackage.InternalExt)
		}
		// log.Println(len(payloadPackage.MessagesList))
		for _, msg := range payloadPackage.MessagesList {
			switch msg.Method {
			case "WebcastChatMessage":
				r.parseChatMsg(msg.Payload)
			case "WebcastGiftMessage":
				r.parseGiftMsg(msg.Payload)
			case "WebcastLikeMessage":
				r.parseLikeMsg(msg.Payload)
			case "WebcastMemberMessage":
				r.parseEnterMsg(msg.Payload)
			default:
			}
		}
	}
}

func (r *Room) send() {
	for {
		pingPack := &dyproto.PushFrame{
			PayloadType: "bh",
		}
		data, _ := proto.Marshal(pingPack)
		err := r.wsConnect.WriteMessage(websocket.BinaryMessage, data)
		if err != nil {
			panic(err.Error())
		}
		// log.Println("[系统] 发送 ping 包")
		time.Sleep(time.Second * 10)
	}
}

func (r *Room) sendAck(logId uint64, iExt string) {
	ackPack := &dyproto.PushFrame{
		LogId:       logId,
		PayloadType: iExt,
	}
	data, _ := proto.Marshal(ackPack)
	err := r.wsConnect.WriteMessage(websocket.BinaryMessage, data)
	if err != nil {
		panic(err.Error())
	}
	// log.Println("[系统] 发送 ack 包")
}

func degzip(data []byte) ([]byte, error) {
	b := bytes.NewReader(data)
	var out bytes.Buffer
	r, err := gzip.NewReader(b)
	if err != nil {
		return nil, err
	}
	_, err = io.Copy(&out, r)
	if err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (r *Room) parseChatMsg(msg []byte) {
	chatMsg := &dyproto.ChatMessage{}
	_ = proto.Unmarshal(msg, chatMsg)
	// log.Printf("[弹幕] %s : %s", chatMsg.User.NickName, chatMsg.Content)
	for _, fn := range r.fns {
		go fn(chatMsg)
	}
}

func (r *Room) parseGiftMsg(msg []byte) {
	giftMsg := &dyproto.GiftMessage{}
	_ = proto.Unmarshal(msg, giftMsg)
	// log.Printf("[礼物] %s : %s * %d, %v, %v, 重复 %v \n", giftMsg.User.NickName, giftMsg.Gift.Name, giftMsg.ComboCount, giftMsg.Gift.Combo, giftMsg.SendTime, giftMsg.RepeatEnd)
	for _, fn := range r.fns {
		go fn(giftMsg)
	}
}

func (r *Room) parseLikeMsg(msg []byte) {
	likeMsg := &dyproto.LikeMessage{}
	_ = proto.Unmarshal(msg, likeMsg)
	// log.Printf("[点赞] %s 点赞 * %d \n", likeMsg.User.NickName, likeMsg.Count)
	for _, fn := range r.fns {
		go fn(likeMsg)
	}
}

func (r *Room) parseEnterMsg(msg []byte) {
	enterMsg := &dyproto.MemberMessage{}
	_ = proto.Unmarshal(msg, enterMsg)
	// log.Printf("[入场] %s 直播间\n", enterMsg.User.NickName)
	for _, fn := range r.fns {
		go fn(enterMsg)
	}
}
