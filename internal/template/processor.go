package template

import (
	"os"
	"io/ioutil"
	"errors"
	"log"
	"sync"
	"encoding/json"
	"net/http"
	"bytes"
	"strings"
	"regexp"
	"text/template"
	"crypto/tls"
	"github.com/naoina/toml"
	"github.com/ltkh/jiramanager/internal/db"
	"github.com/ltkh/jiramanager/internal/config"
)

type Template struct {
	Alerts       Alerts
	Jira         Jira
}

type Alerts struct {
	Api          string
	Login        string
	Passwd       string
}

type Jira struct {
	Api          string
	Dir          string 
	Src          string
	Login        string
	Passwd       string
}

type Data struct {
	Status       string                  
	Error        string                  
	Data struct {
		Alerts   []map[string]interface{}
	}            
}

type Create struct {
	Id           string
	Key          string
	Self         string
}

type Issue struct {
	Fields struct {
		Status struct {
			Id   string
			Name string
		}
	}
}

func Request(method, url string, data []byte, login, passwd string) ([]byte, error){

	//ignore certificate
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	req, err := http.NewRequest(method, url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}

	if login != "" && passwd != "" {
        req.SetBasicAuth(login, passwd)
	}

	if method == "POST" || method == "PUT" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, errors.New(string(body))
	}

	return body, nil
}

func New(filename string, tmpl Template) (*Template, error) {
	f, err := os.Open(filename)
	if err != nil {
		return &tmpl, err
	}
	defer f.Close()

	if err := toml.NewDecoder(f).Decode(&tmpl); err != nil {
		return &tmpl, err
	}

	return &tmpl, nil
}

func (tl *Template) getAlerts(cfg *config.Config) ([]map[string]interface{}, error) {
	
	body, err := Request("GET", tl.Alerts.Api, nil, tl.Alerts.Login, tl.Alerts.Passwd)
    if err != nil {
		return nil, err
	}
	
	var resp Data
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, errors.New(string(body))
	}

	return resp.Data.Alerts, nil
}

func (tl *Template) newTemplate(def string, alert map[string]interface{}) ([]byte, error) {

	//log.Printf("%#v", alert)

	funcMap := template.FuncMap{
		"toInt":           toInt,
		"toFloat":         toFloat,
		"add":             addFunc,
		"replace":         strings.Replace,
		"regexReplace":    regexReplaceAll,
		"lookupIP":        LookupIP,
	    "lookupIPV4":      LookupIPV4,
		"lookupIPV6":      LookupIPV6,
		"strQuote":        strQuote,
		"base":            path.Base,
		"split":           strings.Split,
		"json":            UnmarshalJsonObject,
		"jsonArray":       UnmarshalJsonArray,
		"dir":             path.Dir,
		"map":             CreateMap,
		"getenv":          Getenv,
		"join":            strings.Join,
		"datetime":        time.Now,
		"toUpper":         strings.ToUpper,
		"toLower":         strings.ToLower,
		"contains":        strings.Contains,
		"replace":         strings.Replace,
		"trimSuffix":      strings.TrimSuffix,
		"lookupSRV":       LookupSRV,
		"fileExists":      util.IsFileExist,
		"base64Encode":    Base64Encode,
		"base64Decode":    Base64Decode,
		"parseBool":       strconv.ParseBool,
		"reverse":         Reverse,
		"sortByLength":    SortByLength,
		"sortKVByLength":  SortKVByLength,
		"sub":             func(a, b int) int { return a - b },
		"div":             func(a, b int) int { return a / b },
		"mod":             func(a, b int) int { return a % b },
		"mul":             func(a, b int) int { return a * b },
		"seq":             Seq,
		"atoi":            strconv.Atoi,
	}

	tmpl, err := template.New(tl.Jira.Src).Funcs(funcMap).ParseFiles(tl.Jira.Dir+"/"+tl.Jira.Src)
	if err != nil {
		return nil, err
	}

	var tpl bytes.Buffer
	if err = tmpl.ExecuteTemplate(&tpl, def, &alert); err != nil {
		return nil, err
	}

	return tpl.Bytes(), nil
}

func (tl *Template) createTask(data []byte, groupId string, cfg *config.Config, clnt db.DbClient) (config.Task, error) {
	
	var task config.Task

	body, err := Request("POST", tl.Jira.Api, data, tl.Jira.Login, tl.Jira.Passwd)
    if err != nil {
		return task, err
	}
	
	var resp *Create
	if err := json.Unmarshal(body, &resp); err != nil {
		return task, err
	}

	//set a record from the database
	task = config.Task{
		Group_id:  groupId,
		Task_id:   resp.Id,
		Task_key:  resp.Key,
		Task_self: resp.Self,
		Template:  tl.Jira.Src,
	}

	if err := clnt.SaveTask(task); err != nil {
		return task, err
	}

	if _, err := UpdateTaskStatus(task, cfg, clnt); err != nil {
		return task, err
	}

	return task, nil
}

func (tl *Template) updateTask(data []byte, groupId, method, href, self string, cfg *config.Config, clnt db.DbClient) error {

	_, err := Request(method, href, data, tl.Jira.Login, tl.Jira.Passwd)
	if err != nil {
		return err
	}

	//set a record from the database
	task := config.Task{
		Group_id:  groupId,
		Task_self: self,
		Template:  tl.Jira.Src,
	}

	if method == "POST" {
		if _, err := UpdateTaskStatus(task, cfg, clnt); err != nil {
			return err
		}
	}

	return nil
}

func UpdateTaskStatus(task config.Task, cfg *config.Config, clnt db.DbClient) (Issue, error) {

	var issue Issue

	body, err := Request("GET", task.Task_self+"?fields=status", nil, cfg.Jira.Login, cfg.Jira.Passwd)
	if err != nil {
		return issue, err
	}
	
	if err := json.Unmarshal(body, &issue); err != nil {
		return issue, err
	}

	if err := clnt.UpdateStatus(task.Group_id, issue.Fields.Status.Id, issue.Fields.Status.Name); err != nil {
		return issue, err
	}

	return issue, nil
}

func Process(cfg *config.Config, clnt db.DbClient, test *string) error {

    paths, err := ioutil.ReadDir(cfg.Server.Conf_dir+"/conf.d")
	if err != nil {
		return err
	}
	
	if len(paths) < 1 {
		return errors.New("found no templates")
	}

	template := Template{
		Alerts: Alerts{
			Login:      cfg.Alerts.Login,
			Passwd:     cfg.Alerts.Passwd,
		},
		Jira: Jira{
            Api:        cfg.Jira.Api,
			Dir:        cfg.Server.Conf_dir+"/templates/",
			Login:      cfg.Jira.Login,
			Passwd:     cfg.Jira.Passwd,
		},
	}

	var wg sync.WaitGroup

	for _, p := range paths {

        wg.Add(1)

		go func(wg *sync.WaitGroup, filename string, cfg *config.Config, clnt db.DbClient, template Template, test *string){

			defer wg.Done()

			tmpl, err := New(cfg.Server.Conf_dir+"/conf.d/"+filename, template)
			if err != nil {
				log.Printf("[error] %v", err)
				return
			}

			alrts, err := tmpl.getAlerts(cfg)
			if err != nil {
				log.Printf("[error] %v", err)
				return
			}

			re := regexp.MustCompile(`^[\r\n\t\s]*$`)

			for _, alrt := range alrts {

				groupId := alrt["groupId"].(string)

				if groupId == "" {
					log.Print("[error] undefined field groupId")
					continue
				}
				
				//get a record from the database
				task, err := clnt.LoadTask(groupId)
				if err != nil {
					log.Printf("[error] %v", err)
					continue
				}

				if task.Group_id == "" {

					//generate template
					crt, err := tmpl.newTemplate("create", alrt)
					if err != nil {
						log.Printf("[error] template create %v", err)
					} else if re.Match(crt) == false {
                        //created new task
						tsk, err := tmpl.createTask(crt, groupId, cfg, clnt)
						if err != nil {
							log.Printf("[error] task create: %v", err)
						} else {
                            log.Printf("[info] task created: %s", tsk.Task_self)
						}
					}

				} else {

					alrt["issue"] = map[string]interface{}{ 
						"status_id": task.Status_id,
						"status_name": task.Status_name,
					}
					
					//generate template
					upd, err := tmpl.newTemplate("update", alrt)
					if err != nil {
						log.Printf("[error] %v", err)
					} else if re.Match(upd) == false {
						//updated task
						if err := tmpl.updateTask(upd, groupId, "PUT", task.Task_self, task.Task_self, cfg, clnt); err != nil {
							log.Printf("[error] task update id %s: %v", task.Task_id, err)
						} else {
							log.Printf("[info] task updated: %s", task.Task_self)
						}
					}
					
					//generate template
					trn, err := tmpl.newTemplate("transit", alrt)
					if err != nil {
						log.Printf("[error] %v", err)
					} else if re.Match(trn) == false {
                        //updated task
						if err := tmpl.updateTask(trn, groupId, "POST", task.Task_self+"/transitions", task.Task_self, cfg, clnt); err != nil {
							log.Printf("[error] task update id %s: %v", task.Task_id, err)
						} else {
							log.Printf("[info] task updated: %s", task.Task_self)
						}
					}

				}
			}

		}(&wg, p.Name(), cfg, clnt, template, test)
	}

	wg.Wait()
	
	return nil
}