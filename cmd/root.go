/*
 *
 * k6 - a next-generation load testing tool
 * Copyright (C) 2016 Load Impact
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Affero General Public License as
 * published by the Free Software Foundation, either version 3 of the
 * License, or (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package cmd

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	stdlog "log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fatih/color"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/loadimpact/k6/lib/consts"
	"github.com/loadimpact/k6/log"
)

var BannerColor = color.New(color.FgCyan)

//TODO: remove these global variables
//nolint:gochecknoglobals
var (
	outMutex  = &sync.Mutex{}
	stdoutTTY = isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
	stderrTTY = isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())
	stdout    = &consoleWriter{colorable.NewColorableStdout(), stdoutTTY, outMutex, nil}
	stderr    = &consoleWriter{colorable.NewColorableStderr(), stderrTTY, outMutex, nil}
)

const defaultConfigFileName = "config.json"

//TODO: remove these global variables
//nolint:gochecknoglobals
var defaultConfigFilePath = defaultConfigFileName // Updated with the user's config folder in the init() function below
//nolint:gochecknoglobals
var configFilePath = os.Getenv("K6_CONFIG") // Overridden by `-c`/`--config` flag!

//nolint:gochecknoglobals
var (
	// TODO: have environment variables for configuring these? hopefully after we move away from global vars though...
	verbose   bool
	quiet     bool
	noColor   bool
	logOutput string
	logFmt    string
	address   string
)

// This is get all for the main/root k6 command struct
type command struct {
	ctx            context.Context
	logger         *logrus.Logger
	fallbackLogger logrus.FieldLogger
	cmd            *cobra.Command
	loggerStopped  <-chan struct{}
	loggerIsRemote bool
}

func newCommand(ctx context.Context, logger *logrus.Logger, fallbackLogger logrus.FieldLogger) *command {
	c := &command{
		ctx:            ctx,
		logger:         logger,
		fallbackLogger: fallbackLogger,
	}
	// RootCmd represents the base command when called without any subcommands.
	c.cmd = &cobra.Command{
		Use:               "k6",
		Short:             "a next-generation load generator",
		Long:              BannerColor.Sprintf("\n%s", consts.Banner()),
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: c.persistentPerRunE,
	}
	return c
}

func (c *command) persistentPerRunE(cmd *cobra.Command, args []string) error {
	if !cmd.Flags().Changed("log-output") {
		if envLogOutput, ok := os.LookupEnv("K6_LOG_OUTPUT"); ok {
			logOutput = envLogOutput
		}
	}
	var err error
	c.loggerStopped, err = setupLoggers(c.ctx, c.logger, c.fallbackLogger, logFmt, logOutput)
	if err != nil {
		return err
	}
	select {
	case <-c.loggerStopped:
	default:
		c.loggerIsRemote = true
	}

	if noColor {
		// TODO: figure out something else... currently, with the wrappers
		// below, we're stripping any colors from the output after we've
		// added them. The problem is that, besides being very inefficient,
		// this actually also strips other special characters from the
		// intended output, like the progressbar formatting ones, which
		// would otherwise be fine (in a TTY).
		//
		// It would be much better if we avoid messing with the output and
		// instead have a parametrized instance of the color library. It
		// will return colored output if colors are enabled and simply
		// return the passed input as-is (i.e. be a noop) if colors are
		// disabled...
		stdout.Writer = colorable.NewNonColorable(os.Stdout)
		stderr.Writer = colorable.NewNonColorable(os.Stderr)
	}
	stdlog.SetOutput(c.logger.Writer())
	c.logger.Debugf("k6 version: v%s", consts.FullVersion())
	return nil
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() { //nolint:funlen
	logger := &logrus.Logger{
		Out:       os.Stderr,
		Formatter: new(logrus.TextFormatter),
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.InfoLevel,
	}

	var fallbackLogger logrus.FieldLogger = &logrus.Logger{
		Out:       os.Stderr,
		Formatter: new(logrus.TextFormatter),
		Hooks:     make(logrus.LevelHooks),
		Level:     logrus.InfoLevel,
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newCommand(ctx, logger, fallbackLogger)
	confDir, err := os.UserConfigDir()
	if err != nil {
		logrus.WithError(err).Warn("could not get config directory")
		confDir = ".config"
	}
	defaultConfigFilePath = filepath.Join(
		confDir,
		"loadimpact",
		"k6",
		defaultConfigFileName,
	)

	c.cmd.PersistentFlags().AddFlagSet(rootCmdPersistentFlagSet())

	archiveCmd := getArchiveCmd(logger)
	c.cmd.AddCommand(archiveCmd)
	archiveCmd.Flags().SortFlags = false
	archiveCmd.Flags().AddFlagSet(archiveCmdFlagSet())

	cloudCmd := getCloudCmd(c.ctx, logger)
	c.cmd.AddCommand(cloudCmd)
	cloudCmd.Flags().SortFlags = false
	cloudCmd.Flags().AddFlagSet(cloudCmdFlagSet())

	c.cmd.AddCommand(getConvertCmd())

	inspectCmd := getInspectCmd(logger)
	c.cmd.AddCommand(inspectCmd)
	inspectCmd.Flags().SortFlags = false
	inspectCmd.Flags().AddFlagSet(runtimeOptionFlagSet(false))
	inspectCmd.Flags().StringVarP(&runType, "type", "t", runType, "override file `type`, \"js\" or \"archive\"")

	loginCmd := getLoginCmd()
	c.cmd.AddCommand(loginCmd)

	loginCloudCommand := getLoginCloudCommand(logger)
	loginCmd.AddCommand(loginCloudCommand)
	loginCloudCommand.Flags().StringP("token", "t", "", "specify `token` to use")
	loginCloudCommand.Flags().BoolP("show", "s", false, "display saved token and exit")
	loginCloudCommand.Flags().BoolP("reset", "r", false, "reset token")

	loginCmd.AddCommand(getLoginInfluxDBCommand(logger))

	c.cmd.AddCommand(getPauseCmd(c.ctx))

	c.cmd.AddCommand(getResumeCmd(c.ctx))

	scaleCmd := getScaleCmd(c.ctx)
	c.cmd.AddCommand(scaleCmd)

	scaleCmd.Flags().Int64P("vus", "u", 1, "number of virtual users")
	scaleCmd.Flags().Int64P("max", "m", 0, "max available virtual users")

	runCmd := getRunCmd(c.ctx, logger)
	c.cmd.AddCommand(runCmd)

	runCmd.Flags().SortFlags = false
	runCmd.Flags().AddFlagSet(runCmdFlagSet())

	c.cmd.AddCommand(getStatsCmd(c.ctx))

	c.cmd.AddCommand(getStatusCmd(c.ctx))

	c.cmd.AddCommand(getVersionCmd())

	if err := c.cmd.Execute(); err != nil {
		code := -1

		var fields logrus.Fields
		if e, ok := err.(ExitCode); ok {
			code = e.Code
			if e.Hint != "" {
				fields["hint"] = e.Hint
			}
		}

		logger.WithFields(fields).Error(err)
		if c.loggerIsRemote {
			fallbackLogger.WithFields(fields).Error(err)
			cancel()
			<-c.loggerStopped // TODO have a timeout?
		}

		os.Exit(code)
	}

	if c.loggerIsRemote {
		cancel()
		<-c.loggerStopped // TODO have a timeout?
	}
}

func rootCmdPersistentFlagSet() *pflag.FlagSet {
	flags := pflag.NewFlagSet("", pflag.ContinueOnError)
	// TODO: figure out a better way to handle the CLI flags - global variables are not very testable... :/
	flags.BoolVarP(&verbose, "verbose", "v", false, "enable debug logging")
	flags.BoolVarP(&quiet, "quiet", "q", false, "disable progress updates")
	flags.BoolVar(&noColor, "no-color", false, "disable colored output")
	flags.StringVar(&logOutput, "log-output", "stderr",
		"change the output for k6 logs, possible values are stderr,stdout,none,loki[=host:port]")
	flags.StringVar(&logFmt, "logformat", "", "log output format") // TODO rename to log-format and warn on old usage
	flags.StringVarP(&address, "address", "a", "localhost:6565", "address for the api server")

	// TODO: Fix... This default value needed, so both CLI flags and environment variables work
	flags.StringVarP(&configFilePath, "config", "c", configFilePath, "JSON config file")
	// And we also need to explicitly set the default value for the usage message here, so things
	// like `K6_CONFIG="blah" k6 run -h` don't produce a weird usage message
	flags.Lookup("config").DefValue = defaultConfigFilePath
	must(cobra.MarkFlagFilename(flags, "config"))
	return flags
}

// fprintf panics when where's an error writing to the supplied io.Writer
func fprintf(w io.Writer, format string, a ...interface{}) (n int) {
	n, err := fmt.Fprintf(w, format, a...)
	if err != nil {
		panic(err.Error())
	}
	return n
}

// RawFormatter it does nothing with the message just prints it
type RawFormatter struct{}

// Format renders a single log entry
func (f RawFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	return append([]byte(entry.Message), '\n'), nil
}

// The returned channel will be closed when the logger has finished flushing and pushing logs after
// the provided context is closed. It is closed if the logger isn't buffering and sending messages
// Asynchronously
func setupLoggers(
	ctx context.Context, logger *logrus.Logger, fallbackLogger logrus.FieldLogger, logFmt, logOutput string,
) (<-chan struct{}, error) {
	ch := make(chan struct{})
	close(ch)

	if verbose {
		logger.SetLevel(logrus.DebugLevel)
	}
	switch logOutput {
	case "stderr":
		logger.SetOutput(stderr)
	case "stdout":
		logger.SetOutput(stdout)
	case "none":
		logger.SetOutput(ioutil.Discard)
	default:
		if !strings.HasPrefix(logOutput, "loki") {
			return nil, fmt.Errorf("unsupported log output `%s`", logOutput)
		}
		ch = make(chan struct{})
		hook, err := log.LokiFromConfigLine(ctx, fallbackLogger, logOutput, ch)
		if err != nil {
			return nil, err
		}
		logger.AddHook(hook)
		logger.SetOutput(ioutil.Discard) // don't output to anywhere else
		logFmt = "raw"
		noColor = true // disable color
	}

	switch logFmt {
	case "raw":
		logger.SetFormatter(&RawFormatter{})
		logger.Debug("Logger format: RAW")
	case "json":
		logger.SetFormatter(&logrus.JSONFormatter{})
		logger.Debug("Logger format: JSON")
	default:
		logger.SetFormatter(&logrus.TextFormatter{ForceColors: stderrTTY, DisableColors: noColor})
		logger.Debug("Logger format: TEXT")
	}
	return ch, nil
}
