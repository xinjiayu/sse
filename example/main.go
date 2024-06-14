package main

import (
	"encoding/json"
	sseserver "github.com/xinjiayu/sse"
	"log"
	"runtime"
	"time"
)

func main() {
	server := sseserver.NewServer()
	go server.Serve(":8082")

	go func() {
		for {
			message := sseserver.SSEMessage{
				Event:     "env",
				Data:      getEnv(),
				Namespace: "/sysenv/update",
			}
			server.Broadcast <- message

			time.Sleep(1 * time.Second)
		}
	}()

	select {}
}

func getEnv() (data []byte) {
	var gm runtime.MemStats
	runtime.ReadMemStats(&gm)
	var tmpData = make(map[string]interface{})
	tmpData["goVersion"] = runtime.Version()
	tmpData["goroutine"] = runtime.NumGoroutine()
	//log.Println("GO运行信息", tmpData)
	data, err := json.Marshal(tmpData)
	if err != nil {
		log.Fatal(err)
	}
	return data
}
