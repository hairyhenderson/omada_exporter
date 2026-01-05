package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"strings"

	"github.com/charlie-haley/omada_exporter/pkg/api"
	"github.com/charlie-haley/omada_exporter/pkg/collector"
	"github.com/charlie-haley/omada_exporter/pkg/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	zerolog "github.com/rs/zerolog"
	log "github.com/rs/zerolog/log"
	"github.com/urfave/cli/v3"
)

var version = "development"

var conf = config.Config{}

func Run() {
	app := cli.Command{}
	app.Name = "omada_exporter"
	app.Version = version
	app.Usage = "Prometheus Exporter for TP-Link Omada Controller SDN."
	app.EnableShellCompletion = true
	app.Authors = []any{
		mail.Address{Name: "Charlie Haley", Address: "charlie-haley@users.noreply.github.com"},
	}
	app.Flags = []cli.Flag{
		&cli.StringFlag{Destination: &conf.Host, Required: true, Name: "host", Value: "", Usage: "The hostname of the Omada Controller, including protocol.", Sources: cli.EnvVars("OMADA_HOST")},
		&cli.StringFlag{Destination: &conf.Username, Required: true, Name: "username", Value: "", Usage: "Username of the Omada user you'd like to use to fetch metrics.", Sources: cli.EnvVars("OMADA_USER")},
		&cli.StringFlag{Destination: &conf.Password, Required: true, Name: "password", Value: "", Usage: "Password for your Omada user.", Sources: cli.EnvVars("OMADA_PASS")},
		&cli.StringFlag{Destination: &conf.Port, Name: "port", Value: "9202", Usage: "Port on which to expose the Prometheus metrics.", Sources: cli.EnvVars("OMADA_PORT")},
		&cli.StringFlag{Destination: &conf.Site, Name: "site", Value: "Default", Usage: "Omada site to scrape metrics from.", Sources: cli.EnvVars("OMADA_SITE")},
		&cli.StringFlag{Destination: &conf.LogLevel, Name: "log-level", Value: "error", Usage: "Application log level.", Sources: cli.EnvVars("LOG_LEVEL")},
		&cli.IntFlag{Destination: &conf.Timeout, Name: "timeout", Value: 15, Usage: "Timeout when making requests to the Omada Controller.", Sources: cli.EnvVars("OMADA_REQUEST_TIMEOUT")},
		&cli.BoolFlag{Destination: &conf.Insecure, Name: "insecure", Value: false, Usage: "Whether to skip verifying the SSL certificate on the controller.", Sources: cli.EnvVars("OMADA_INSECURE")},
		&cli.BoolFlag{Destination: &conf.GoCollectorDisabled, Name: "disable-go-collector", Value: false, Usage: "Disable Go collector metrics.", Sources: cli.EnvVars("OMADA_DISABLE_GO_COLLECTOR")},
		&cli.BoolFlag{Destination: &conf.ProcessCollectorDisabled, Name: "disable-process-collector", Value: false, Usage: "Disable process collector metrics.", Sources: cli.EnvVars("OMADA_DISABLE_PROCESS_COLLECTOR")},
	}
	app.Commands = []*cli.Command{
		{Name: "version", Aliases: []string{"v"}, Usage: "prints the current version.",
			Action: func(_ context.Context, _ *cli.Command) error {
				fmt.Println(version)
				os.Exit(0)
				return nil
			}},
		{Name: "mdocs", Aliases: []string{"md"}, Usage: "prints the metric docs.",
			Action: func(_ context.Context, _ *cli.Command) error {
				mdocs()
				os.Exit(0)
				return nil
			}},
	}
	app.Action = run

	err := app.Run(context.Background(), os.Args)
	if err != nil {
		log.Fatal().Err(err).Msg("App failed to run")
		os.Exit(1)
	}
}

func run(ctx context.Context, cmd *cli.Command) error {
	// set log level
	level, err := zerolog.ParseLevel(conf.LogLevel)
	if err != nil {
		return err
	}
	zerolog.SetGlobalLevel(level)

	if conf.GoCollectorDisabled {
		// remove Go collector
		prometheus.Unregister(collectors.NewGoCollector())
	}

	if conf.ProcessCollectorDisabled {
		// remove Process collector
		prometheus.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	}

	// check if host is properly formatted
	if strings.HasSuffix(conf.Host, "/") {
		// remove trailing slash if it exists
		conf.Host = strings.TrimRight(conf.Host, "/")
	}

	client, err := api.Configure(&conf)
	if err != nil {
		return err
	}

	// register omada collectors
	for _, c := range allCollectors(client) {
		prometheus.MustRegister(c)
	}

	log.Info().Msg(fmt.Sprintf("listening on :%s", conf.Port))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html>
    <head>
	<title>omada_exporter</title>
	</head>
    	<body>
			<h1>omada_exporter</h1>
			<p>
				<a href="/metrics">Metrics</a>
			</p>
    	</body>
    </html>`))
	})

	http.Handle("/metrics", promhttp.Handler())
	err = http.ListenAndServe(fmt.Sprintf(":%s", conf.Port), nil)
	if err != nil {
		return err
	}

	return nil
}

// mdocs just spits out the metrics descriptions and exits
func mdocs() {

	// Describe wants to return descriptions via a channel, so make and fill a channel.
	dc := make(chan *prometheus.Desc)
	go func() {
		// collectors can't Collect without a client, but Describe doesn't need one.
		for _, c := range allCollectors(nil) {
			c.Describe(dc)
		}
		close(dc)
	}()

	_, _ = fmt.Fprintln(os.Stdout, "| Name | Description | Labels |\n|--|--|--|")

	// drain the channel
	for {
		if description := <-dc; description != nil {
			// Sure would be nice if the prometheus.Desc wasn't so opaque. This is gross and fragile.
			d := description.String()
			d = strings.Replace(d, `Desc{fqName: "`, "| ", 1)
			d = strings.Replace(d, `", help: "`, " | ", 1)
			d = strings.Replace(d, `", constLabels: {}, variableLabels: [`, " | ", 1)
			d = strings.Replace(d, `]}`, " | ", 1)
			_, _ = fmt.Fprintln(os.Stdout, d)
		} else {
			break
		}
	}
}

// allCollectors returns the full complement of configured allCollectors.
func allCollectors(client *api.Client) []prometheus.Collector {
	return []prometheus.Collector{
		collector.NewClientCollector(client),
		collector.NewControllerCollector(client),
		collector.NewDeviceCollector(client),
		collector.NewPortCollector(client),
	}
}
