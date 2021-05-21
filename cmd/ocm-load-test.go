package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cloud-bulldozer/ocm-api-load/pkg/cmd"
	"github.com/cloud-bulldozer/ocm-api-load/pkg/helpers"
	"github.com/cloud-bulldozer/ocm-api-load/pkg/logging"
	"github.com/cloud-bulldozer/ocm-api-load/pkg/tests"
	uuid "github.com/satori/go.uuid"
	"github.com/spf13/cobra"

	"github.com/spf13/viper"
)

var (
	configFile  string
	ocmTokenURL string
	ocmToken    string
	testID      string
	outputPath  string
	duration    int
	rate        string
	gatewayUrl  string
	testNames   []string
	verbose     bool
)

const (
	longHelp = `
	A set of load tests for OCM's clusters-service, based on vegeta.
	For example:

	ocm-load-test --test-id=foo --ocm-token=$OCM_TOKEN --duration=20m --rate=5/s --output-path=./results/$TEST_ID_$TEST_NAME.json <test_name>

	Or

	ocm-load-test --config-file=config.yaml
`
)

var rootCmd = &cobra.Command{
	Use:   "ocm-api-load",
	Short: "A set of load tests for OCM's clusters-service, based on vegeta.",
	Long:  longHelp,
	RunE:  run,
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&configFile, "config-file", "config.yaml", "config file")
	rootCmd.PersistentFlags().StringVar(&ocmTokenURL, "ocm-token-url", "https://sso.redhat.com/auth/realms/redhat-external/protocol/openid-connect/token", "Token URL")
	rootCmd.PersistentFlags().StringVar(&ocmToken, "ocm-token", "", "OCM Authorization token")
	rootCmd.PersistentFlags().StringVar(&gatewayUrl, "gateway-url", "https://api.integration.openshift.com", "Gateway url to perform the test against")
	rootCmd.PersistentFlags().StringVar(&testID, "test-id", uuid.NewV4().String(), "Unique ID to identify the test run. UUID is recommended")
	rootCmd.PersistentFlags().StringVar(&outputPath, "output-path", "results", "Output directory for result and report files")
	rootCmd.PersistentFlags().IntVar(&duration, "duration", 1, "Duration of each individual run in minutes.")
	rootCmd.PersistentFlags().StringVar(&rate, "rate", "1/s", "Rate of the attack. Format example 5/s. (Available units 'ns', 'us', 'ms', 's', 'm', 'h')")
	rootCmd.PersistentFlags().StringSliceVar(&testNames, "test-names", []string{}, "Names for the tests to be run.")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "set this flag to activate verbose logging.")
	rootCmd.AddCommand(cmd.NewVersionCommand())
}

func initConfig() {
	viper.SetConfigType("yaml")
	if configFile == "" {
		viper.AddConfigPath(".")
	}
	viper.SetConfigFile(configFile)
	viper.BindPFlags(rootCmd.Flags())

	viper.AutomaticEnv()

	if _, err := os.Stat(configFile); err != nil {
		viper.WriteConfig()
	} else {
		err := viper.ReadInConfig()
		if err != nil {
			panic(fmt.Errorf("fatal error config file: %s", err))
		}
	}
}

func run(cmd *cobra.Command, args []string) error {
	logger, err := logging.NewGoLoggerBuilder().
		Debug(viper.GetBool("verbose")).
		Build()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Can't build logger: %v\n", err)
		os.Exit(1)
	}

	if viper.GetString("ocm-token") == "" {
		logger.Fatal(cmd.Context(), "ocm-token is a necessary configuration")
	}
	err = helpers.CreateFolder(viper.GetString("output-path"), logger)
	if err != nil {
		logger.Fatal(cmd.Context(), "creating api connection: %v", err)
	}
	logger.Info(cmd.Context(), "Using output directory: %s", viper.GetString("output-path"))

	connection, err := helpers.BuildConnection(viper.GetString("gateway-url"),
		viper.GetString("client.id"),
		viper.GetString("client.secret"),
		viper.GetString("ocm-token"),
		logger,
		cmd.Context())
	if err != nil {
		logger.Fatal(cmd.Context(), "creating api connection: %v", err)
	}
	defer helpers.Cleanup(connection)

	vegetaRate, err := helpers.ParseRate(viper.GetString("rate"))
	if err != nil {
		logger.Fatal(cmd.Context(), "parsing rate: %v", err)
	}

	// Flag overrides config
	// Selecting test passed in the Flag
	if len(viper.GetStringSlice("test-names")) > 0 {
		viper.Set("tests", map[string]interface{}{})
		tests := viper.GetStringSlice("test-names")
		for _, t := range tests {
			viper.Set(fmt.Sprintf("tests.%s", t), map[string]interface{}{})
		}
	}

	// If no Flag or Config is passed all test should run
	if len(viper.GetStringSlice("test-names")) == 0 && len(viper.GetStringMap("tests")) == 0 {
		viper.Set("tests.all", map[string]interface{}{})
	}

	if err := tests.Run(viper.GetString("test-id"),
		viper.GetString("output-path"),
		time.Duration(viper.GetInt("duration"))*time.Minute,
		vegetaRate,
		connection,
		viper.Sub("tests"),
		logger,
		cmd.Context()); err != nil {
		logger.Fatal(cmd.Context(), "running load test: %v", err)
	}

	return nil
}

func main() {
	ctx := context.Background()
	if err := rootCmd.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}