package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/chbmuc/cec"
	mqtt "github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	log "github.com/sirupsen/logrus"
)

var (
	brokers  *string
	user     *string
	pass     *string
	clientId *string
	prefix   *string
	refresh  *int

	cecClient  *cec.Connection
	mqttClient *mqtt.ConnectionManager

	activeDevices [16]bool = [16]bool{}

	err error
)

func main() {
	log.Info("starting MQTT-CEC bridge...")

	brokers = flag.String("brokers", "mqtt://localhost:1883", "comma separated URL(s) for the broker (schemes supported include 'mqtt' and 'tls')")
	user = flag.String("user", "", "username to use when connecting to MQTT")
	pass = flag.String("pass", "", "password to use when connecting to MQTT")
	clientId = flag.String("clientid", "mqtt-cec-bridge", "client ID to use when connecting to MQTT")
	prefix = flag.String("prefix", "media", "the bridge will listen to the {prefix}/cec/+/cmd topic")
	refresh = flag.Int("refresh", 5, "amount of seconds between polling device power states")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())

	mqttOpts := mqtt.ClientConfig{
		BrokerUrls:        brokerParamToURLs(*brokers),
		ConnectRetryDelay: 1 * time.Second,
		ConnectTimeout:    1 * time.Second,
		OnConnectionUp:    subscribe,
		OnConnectError:    func(err error) { log.Fatalf("MQTT connect error: %s", err) },
		ClientConfig: paho.ClientConfig{
			ClientID:      *clientId,
			Router:        paho.NewSingleHandlerRouter(handleMQTTMessage),
			OnClientError: func(err error) { log.Errorf("server requested disconnect: %s", err) },
			OnServerDisconnect: func(d *paho.Disconnect) {
				if d.Properties != nil {
					log.Errorf("server requested disconnect: %s", d.Properties.ReasonString)
				} else {
					log.Errorf("server requested disconnect; reason code: %d", d.ReasonCode)
				}
			},
		},
	}
	mqttOpts.SetUsernamePassword(*user, []byte(*pass))

	mqttClient, err = mqtt.NewConnection(ctx, mqttOpts)
	if err != nil {
		log.Fatal(err)
	}

	cecClient, err = cec.Open("", "cec-mqtt", true)
	if err != nil {
		log.Fatalf("Error initializing CEC: %s", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go cecRefresh(ctx, &wg)

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c

	cancel()
	wg.Wait()
}

func subscribe(cm *mqtt.ConnectionManager, ca *paho.Connack) {
	log.Info("MQTT connection up")
	if _, err := cm.Subscribe(context.Background(), &paho.Subscribe{
		Subscriptions: map[string]paho.SubscribeOptions{
			fmt.Sprintf("%s/cec/+/cmd", *prefix): {QoS: 0},
		},
	}); err != nil {
		fmt.Printf("failed to subscribe (%s). This is likely to mean no messages will be received.", err)
		return
	}
}

func connectionError(err error) {
	log.Fatalf("MQTT connection error: %s", err)
}

func handleMQTTMessage(m *paho.Publish) {
	topic := m.Topic
	payload := string(m.Payload)
	log.Infof("received msg on topic '%s': %s", topic, payload)

	topicLevels := strings.Split(topic, "/")
	address, err := strconv.Atoi(topicLevels[len(topicLevels)-2])
	if err != nil {
		log.Errorf("Error handling MQTT message: %s", err)
		return
	}

	if payload == "on" {
		cecClient.PowerOn(address)
	} else if payload == "off" {
		cecClient.Standby(address)
	}
}

func cecRefresh(ctx context.Context, wg *sync.WaitGroup) {
	for {
		select {
		case <-time.After(time.Duration(*refresh) * time.Second):
			activeDevices = cecClient.GetActiveDevices()

			for address, active := range activeDevices {
				if active {
					state := ""
					switch cecClient.GetDevicePowerStatus(address) {
					case "on":
						state = "on"
					case "standby":
						state = "off"
					}

					if state != "" {
						mqttClient.Publish(context.Background(), &paho.Publish{
							QoS:     0,
							Topic:   fmt.Sprintf("%s/cec/%d/state", *prefix, address),
							Payload: []byte(state),
							Retain:  true,
						})
					}
				}
			}
		case <-ctx.Done():
			cecClient.Destroy()
			wg.Done()
			return
		}
	}
}

func brokerParamToURLs(brokers string) []*url.URL {
	urls := make([]*url.URL, 0)

	for _, broker := range strings.Split(brokers, ",") {
		url, err := url.Parse(broker)
		if err != nil {
			log.Fatal(err)
		}

		urls = append(urls, url)
	}

	return urls
}
