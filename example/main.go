package main

import (
	"log"
	"sync"

	"github.com/zboyco/douyin-live-go"
	"github.com/zboyco/douyin-live-go/protobuf"
)

func main() {
	r, err := douyin.NewRoom("793116150352", func(message any) {
		switch msg := message.(type) {
		case *protobuf.GiftMessage:
			log.Printf("[礼物] %s : %s * %d \n", msg.User.NickName, msg.Gift.Name, msg.ComboCount)
		default:
		}
	})
	if err != nil {
		panic(err)
	}
	_ = r.Connect()
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}
