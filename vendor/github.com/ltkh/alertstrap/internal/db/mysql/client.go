package mysql

import (
	"log"
	"fmt"
	"time"
	"strconv"
	"encoding/json"
	"database/sql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/ltkh/alertstrap/internal/config"
	"github.com/ltkh/alertstrap/internal/cache"
)

type Client struct {
	client *sql.DB
    config *config.DB
}

func NewClient(conf *config.DB) (*Client, error) {
	conn, err := sql.Open("mysql", conf.Conn_string)
	if err != nil {
		return nil, err
	}
	return &Client{ client: conn, config: conf }, nil
}

func (db *Client) Close() {
	db.client.Close()
}

func (db *Client) LoadUsers() ([]cache.User, error) {
	result := []cache.User{}

	rows, err := db.client.Query(fmt.Sprintf("select login,password,token from %s", db.config.Users_table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var usr cache.User
		usr.EndsAt = time.Now().UTC().Unix()
        err := rows.Scan(&usr.Login, &usr.Password, &usr.Token)
        if err != nil {
            return nil, err
		}
		result = append(result, usr) 
    }

  	return result, nil
}

func (db *Client) LoadAlerts() ([]cache.Alert, error) {
	result := []cache.Alert{}

	rows, err := db.client.Query(fmt.Sprintf("select * from %s a where a.ends_at = (select max(ends_at) from %s where group_id = a.group_id)", db.config.Alerts_table, db.config.Alerts_table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	// Make a slice for the values
	values := make([]sql.RawBytes, len(columns))

	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	for rows.Next() {
		var a cache.Alert

		a.StampsAt = time.Now().UTC().Unix()

		if err := rows.Scan(scanArgs...); err != nil {
			continue
		}

		for i, value := range values {
			switch columns[i].Name() {
				case "alert_id":
					a.AlertId = string(value)
				case "group_id":
					a.GroupId = string(value)
				case "status":
					a.Status = string(value)
				case "starts_at":
					cl, err := strconv.Atoi(string(value))
					if err == nil {
						a.StartsAt = int64(cl)
					}
				case "ends_at":
					cl, err := strconv.Atoi(string(value))
					if err == nil {
						a.EndsAt = int64(cl)
					}
				case "duplicate":
					cl, err := strconv.Atoi(string(value))
					if err == nil {
						a.Duplicate = int(cl)
					}
				case "labels":
					if err := json.Unmarshal(value, &a.Labels); err != nil {
						log.Printf("[warning] %v (%s)", err, a.AlertId)
					}
				case "annotations":
					if err := json.Unmarshal(value, &a.Annotations); err != nil {
						log.Printf("[warning] %v (%s)", err, a.AlertId)
					}
			}
		}

		result = append(result, a) 
	}

  	return result, nil
}

func (db *Client) SaveAlerts(alerts map[string]cache.Alert) error {

	stmt, err := db.client.Prepare(fmt.Sprintf("replace into %s values (?,?,?,?,?,?,?,?,?)", db.config.Alerts_table))
	if err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	defer stmt.Close()

	cnt := 0

	for _, i := range alerts {

		labels, err := json.Marshal(i.Labels)
		if err != nil {
			log.Printf("[error] %v", err)
			continue
		}

		annotations, err := json.Marshal(i.Annotations)
		if err != nil {
			log.Printf("[error] %v", err)
			continue
		}

		_, err = stmt.Exec(
			i.AlertId,
			i.GroupId,
			i.Status,
			i.StartsAt,
			i.EndsAt,
			i.Duplicate,
			labels,
			annotations,
			i.GeneratorURL,
		)
		if err != nil {
			log.Printf("[error] %v", err)
			continue
		}

		cnt++

	}

	if cnt > 0 {
		log.Printf("[info] alerts recorded in database (%d)", cnt)
	}

	return nil

}

func (db *Client) AddAlert(alert cache.Alert) error {

	stmt, err := db.client.Prepare(fmt.Sprintf("insert into %s values (?,?,?,?,?,?,?,?,?)", db.config.Alerts_table))
	if err != nil {
		return err
	}
	defer stmt.Close()

	labels, err := json.Marshal(alert.Labels)
	if err != nil {
		return err
	}

	annotations, err := json.Marshal(alert.Annotations)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		alert.AlertId,
		alert.GroupId,
		alert.Status,
		alert.StartsAt,
		alert.EndsAt,
		alert.Duplicate,
		labels,
		annotations,
		alert.GeneratorURL,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *Client) UpdAlert(alert cache.Alert) error {

	stmt, err := db.client.Prepare(fmt.Sprintf("update %s set status=?,ends_at=?,duplicate=?,annotations=?,generator_url=? where alert_id = ?", db.config.Alerts_table))
	if err != nil {
		return err
	}
	defer stmt.Close()

	annotations, err := json.Marshal(alert.Annotations)
	if err != nil {
		return err
	}

	_, err = stmt.Exec(
		alert.Status,
		alert.EndsAt,
		alert.Duplicate,
		annotations,
		alert.GeneratorURL,
		alert.AlertId,
	)
	if err != nil {
		return err
	}

	return nil
}

func (db *Client) DeleteOldAlerts() error {

	stmt, err := db.client.Prepare(fmt.Sprintf("delete from %s where ends_at < UNIX_TIMESTAMP() - 86400 * ?", db.config.Alerts_table))
	if err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	defer stmt.Close()

	_, err = stmt.Exec(db.config.History_days)
	if err != nil {
		log.Printf("[error] %v", err)
		return err
	}

	return nil

}