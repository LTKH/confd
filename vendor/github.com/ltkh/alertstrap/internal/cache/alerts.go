package cache

import (
	"sync"
    "time"
    "log"
)

type Alerts struct {
	sync.RWMutex
    items           map[string]Alert
}

type Alert struct {
    AlertId         string                  
    GroupId         string                  
	Status          string                  
    StartsAt        int64                   
	EndsAt          int64                   
	StampsAt        int64                   
    Duplicate       int                     
    Labels          map[string]interface{}  
    Annotations     map[string]interface{}  
    GeneratorURL    string                  
}

func NewCacheAlerts() *Alerts {

    cache := Alerts{
        items: make(map[string]Alert),
    }

    return &cache
}

func (a *Alerts) Set(key string, value Alert) {

    a.Lock()
    defer a.Unlock()

    a.items[key] = value

}

func (a *Alerts) Get(key string) (Alert, bool) {

    a.RLock()
    defer a.RUnlock()

    item, found := a.items[key]

    if !found {
        return Alert{}, false
    }

    return item, true
}

func (a *Alerts) Delete(key string) {

    a.Lock()
    defer a.Unlock()

    if _, found := a.items[key]; !found {
        log.Printf("[error] key not found in cache (%s)", key)
        return
    }

    delete(a.items, key)

}

// Copies all unexpired items in the cache into a new map and returns it.
func (a *Alerts) Items() map[string]Alert {

	a.RLock()
    defer a.RUnlock()
    
	items := make(map[string]Alert, len(a.items))
	for k, v := range a.items {
		items[k] = v
    }
    
	return items
}

//cleaning cache items
func (a *Alerts) ClearItems(items map[string]Alert) {

    a.Lock()
    defer a.Unlock()

    for k, _ := range items {
        delete(a.items, k)
    }
}

func (a *Alerts) ExpiredItems() map[string]Alert {

    a.RLock()
    defer a.RUnlock()

    items := make(map[string]Alert)

    for k, v := range a.items {
        if time.Now().UTC().Unix() > v.EndsAt + 600 {
            items[k] = v
        }
    }

    return items
}

func (a *Alerts) ResolvedItems() []string {
    a.Lock()
	defer a.Unlock()

	var keys []string

	for k, v := range a.items {
		if v.Status != "resolved" && time.Now().UTC().Unix() > v.EndsAt {
			v.Status = "resolved"
			a.items[k] = v
			keys = append(keys, k)
        }
    }

    return keys
}