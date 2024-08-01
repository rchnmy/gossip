package main

import (
    "time"
    "syscall"
    "context"
    "net/http"
    "os/signal"

    "github.com/rchnmy/gossip/proxy"
)

func main() {
    p := proxy.Deploy()
    m := http.NewServeMux()
    m.HandleFunc("/", proxy.Bounce(p))
    m.HandleFunc("/health", proxy.Probe)

    c, done := signal.NotifyContext(context.Background(),
        syscall.SIGINT,
        syscall.SIGHUP,
        syscall.SIGTERM,
    )
    go proxy.Serve(c, m)

    t := time.NewTicker(3 * time.Hour)
    d, _ := time.ParseDuration("8h")
    for {
        select {
        case <- t.C:
            p.Wipe(d)
        case <- c.Done():
            done()
            return
        }
    }
}

