package main

import (
    "context"
    "log"
	"time"
	"fmt"

    "go.etcd.io/etcd/client/v2"
)

func main() {
	writeClient, _ := client.New(client.Config{
        Endpoints:               []string{"http://127.0.0.1:2379"},
        HeaderTimeoutPerRequest: 5 * time.Second,
    })

    kapi := client.NewKeysAPI(writeClient)

	arr := []string{
		"/ps/hosts",
		"/ps/hosts/test01", 
		"/ps/hosts/test02", 
		"/ps/hosts/test03", 
		"/ps/hosts/test04", 
		"/ps/hosts/test05", 
		"/ps/hosts/test06", 
		"/ps/hosts/test07",
	}
    for _, val := range arr {
        opts := &client.SetOptions{ Dir: true }
		resp, err := kapi.Set(context.Background(), val, "", opts)
		if err != nil { 
			log.Printf("%v", err) 
		} else { 
			log.Printf("%v", resp) 
		}
		for i := 0; i <= 500; i++ {
			if val == "/ps/hosts" {
				continue
			}
			resp, err := kapi.Set(context.Background(), fmt.Sprintf("%s/test-host%d/cmdb", val, i), "{}", &client.SetOptions{})
		    if err != nil { 
				log.Printf("%v", err) 
			} else { 
				log.Printf("%v", resp) 
			}
		}
    }

	
	

    //log.Println("Directory /mydir/ created")
}