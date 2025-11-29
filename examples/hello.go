package main

import (
	"fmt"
	"time"
)

func main() {
	fmt.Println("Hello from dzjjy deployment!")
	fmt.Println("Application started at:", time.Now().Format("2006-01-02 15:04:05"))

	// 保持运行
	for {
		fmt.Println("Running...", time.Now().Format("15:04:05"))
		time.Sleep(5 * time.Second)
	}
}
