package main

import (
	"fmt"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func generateAnonymousName() string {
	return fmt.Sprintf("Anonymous%04d", rand.Intn(10000))
}
