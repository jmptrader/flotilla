+++
title = "start"
description = "Getting Started"
draft = "false"
+++
## Installation<a name="installation"></a>

Flotilla has one dependency, for ensuring a default Templator is provided:

- [djinn](http://github.com/thrisp/djinn/) *templating*

Flotilla can be installed with `go get -u github.com/thrisp/flotilla`.


## Example<a name="example"></a>

A simple example application using Flotilla.


main.go

    package main

    import (
        "math/rand"
        "os"
        "os/signal"

        "github.com/thrisp/flotilla"
    )

    var lucky = []rune("1234567890")

    func randSeq(n int) string {
        b := make([]rune, n)
        for i := range b {
            b[i] = lucky[rand.Intn(len(lucky))]
        }
        return string(b)
    }

    func Display(f flotilla.Ctx) {
        ret := fmt.Sprintf("Your lucky number is: %s", randSeq(20))
        f.Call("serveplain", ret)
    }

    func Lucky() *flotilla.App {
        a := flotilla.New()
        a.GET("/quick/:start", Display)
        a.Configure()
        return a
    }

    var quit = make(chan bool)

    func init() {
        c := make(chan os.Signal, 1)
        signal.Notify(c, os.Interrupt)
        go func() {
            for _ = range c {
                quit <- true
            }
        }()
    }

    func main() {
        app := Lucky()
        go app.Run(":8080")
        <-quit
    }

go run main.go & visit: http://localhost:8080/quick/whatsmyluckynumber