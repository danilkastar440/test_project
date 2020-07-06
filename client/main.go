package main

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jessevdk/go-flags"
	"github.com/rs/zerolog/log"
	uuid "github.com/satori/go.uuid"

	"github.com/danilkastar440/TRRP_LAST/pkg/models"
)

var botId string

type options struct {
	//TODO: change url func
	DispatcherURL string `long:"dispatcher-host" env:"DISPATCHER" required:"true" default:"trrp-virus.ew.r.appspot.com"`
	Interval      string `long:"interval" env:"INTERVAL" required:"true" default:"10s"`
}

func getId() {
	var res string
	id, err := ioutil.ReadFile("./tmp")
	if err != nil {
		res = uuid.NewV4().String()
		_ = ioutil.WriteFile("./tmp", id, os.FileMode(0777))
	} else {
		uid, err := uuid.FromString(string(id))
		if err != nil {
			res = uuid.NewV4().String()
			_ = ioutil.WriteFile("./tmp", id, os.FileMode(0777))
		} else {
			res = uid.String()
		}
	}
	botId = res
}

func subscribeForCommands(dispatcherHost string) error {
	u := url.URL{Scheme: "wss", Host: dispatcherHost, Path: "/subscribe"}

	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer c.Close()

	cnt := 0
	var msg models.AgentDataReq
	client := &http.Client{}
	for {
		if err := c.ReadJSON(&msg); err != nil {
			return err
		}
		cnt++
		log.Info().Msgf("Got %d msg", cnt)

		r := bytes.NewReader(msg.Def.Body)

		agentDataRes := models.AgentDataRes{
			BotId:     botId,
			Def:       msg.Def,
			RequestId: msg.RequestId,
		}
		// timeout should be greater than 2 sec
		timeout, err := time.ParseDuration(msg.Def.Timeout)
		if err != nil {
			agentDataRes.Err = err.Error()
			log.Error().Err(err).Msg("Failed to parse timeout")
			if err := c.WriteJSON(agentDataRes); err != nil {
				return err
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, msg.Def.HttpMethod, msg.Def.Endpoint, r)
		if err != nil {
			agentDataRes.Err = err.Error()
			log.Error().Err(err).Msg("Failed to create request")
			if err := c.WriteJSON(agentDataRes); err != nil {
				return err
			}
			continue
		}

		for k, v := range msg.Def.Headers {
			req.Header.Add(k, v)
		}

		start := time.Now()

		resp, err := client.Do(req)
		if err != nil {
			agentDataRes.Err = err.Error()
			log.Error().Err(err).Msg("Failed to do request")
			if err := c.WriteJSON(agentDataRes); err != nil {
				return err
			}
			continue
		}

		agentDataRes.RequestTime = time.Now().Sub(start).String()

		data, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			agentDataRes.Err = err.Error()
			log.Error().Err(err).Msg("Failed to read data from request")
			if err := c.WriteJSON(agentDataRes); err != nil {
				return err
			}
			continue
		}
		agentDataRes.EndpointData = data
		agentDataRes.StatusCode = resp.StatusCode

		if err := c.WriteJSON(agentDataRes); err != nil {
			return err
		}
		log.Info().Msgf("Sent to router: %#v", agentDataRes)
	}
}

func main() {
	// var link = "https://www.cbr-xml-daily.ru/daily_json.js"
	var opts options
	if _, err := flags.Parse(&opts); err != nil {
		log.Fatal().Err(err).Msg("Failed to parse opts")
	}
	getId()

	interval, err := time.ParseDuration(opts.Interval)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to parse duration from opts")
	}

	ticker := time.NewTicker(interval)
	errs := make(chan error, 1)
	errs <- subscribeForCommands(opts.DispatcherURL)
	for {
		select {
		case err := <-errs:
			if err != nil {
				log.Error().Err(err).Msg("Failed to subscribe for commands")
			}
		case <-ticker.C:
			log.Info().Msg("Tried to reconnect")
			errs <- subscribeForCommands(opts.DispatcherURL)
		}
	}
}
