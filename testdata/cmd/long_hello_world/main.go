package main

import (
	"fmt"
	"time"
)

func main() {
	for i := 1; 5 >= i; i++ {
		time.Sleep(1 * time.Second)
		fmt.Printf("hello from jctl %d\n", i)
	}
}
