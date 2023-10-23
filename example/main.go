package main

import (
	"sync"

	"github.com/zboyco/douyin-live-go"
)

func main() {
	r, err := douyin.NewRoom("5893162289", func(message any) {
		// switch msg := message.(type) {
		// case *dyproto.GiftMessage:
		// 	log.Printf("[礼物] %s : %s * %d \n", msg.User.NickName, msg.Gift.Name, msg.ComboCount)
		// default:
		// }
	})
	if err != nil {
		panic(err)
	}
	_ = r.Connect()
	var wg sync.WaitGroup
	wg.Add(1)
	wg.Wait()
}
