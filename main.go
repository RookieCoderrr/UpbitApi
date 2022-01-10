package main

import (
	"context"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/robfig/cron"
	"net/http"
)

func main() {
	fmt.Println("======Server Start======")
	ctx:= context.TODO()
	_ = initializeRedisLocalClient(ctx)
	setSymbolId()

	c := cron.New()
	err := c.AddFunc("@daily", func() {
		fmt.Println("Start cron")
		go setSymbolId()
	})
	if err != nil {
		fmt.Println("Error starting cron!")
	}
	c.Start()
	muxRouter := mux.NewRouter()
	muxRouter.HandleFunc("/api/{symbol}/info",handler)
	http.ListenAndServe("0.0.0.0:1928",muxRouter)
}
