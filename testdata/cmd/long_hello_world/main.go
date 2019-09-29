package main

import (
	"fmt"
	"time"
)

func main() {
	for i := 1; 30 >= i; i++ {
		time.Sleep(1)
		fmt.Printf("hello from jctl %d", i)
	}
}
