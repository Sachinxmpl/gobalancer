package main

import (
	"flag"
	"fmt"

	"github.com/Sachinxmpl/gobalancer/internal/config"
)

func Check(args []string) error{
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	path := fs.String("c", "config.yaml", "path to the config file")
	if err := fs.Parse(args); err != nil{
		return err 
	}

	if _, err := config.Load(*path); err != nil{
		return err 
	}

	fmt.Printf("%s: ok\n", *path)
	return nil 
}