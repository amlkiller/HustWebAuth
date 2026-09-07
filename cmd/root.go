/*
Copyright © 2022 a76yyyy q981331502@163.com

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

// A program used to implement Ruijie web authentication
package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	daemon "github.com/sevlyar/go-daemon"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile        string
	account        string
	password       string
	serviceType    string
	encrypt        bool
	iface          string
	cooldown       time.Duration
	maxCooldown    time.Duration
	rotationEnable bool
	pingIP         string
	pingCount      int
	pingTimeout    time.Duration
	pingPrivilege  bool
	redirectURL    string
	logDir         string
	logFile        string
	logRandom      bool
	logAppend      bool
	sysLog         bool
	saveCfg        bool
	daemonEnable   bool
	daemonPidFile  string
	cycleEnable    bool
	cycleDuration  time.Duration
	cycleRetry     int
	logConnected   bool
)

// InterfaceConfig holds configuration for an individual network interface worker.
type InterfaceConfig struct {
	Iface       string        `json:"iface" yaml:"iface" mapstructure:"iface"`
	PingIP      string        `json:"pingIP,omitempty" yaml:"pingIP,omitempty" mapstructure:"pingIP"`
	Accounts    []Account     `json:"accounts,omitempty" yaml:"accounts,omitempty" mapstructure:"accounts"`
	Cooldown    time.Duration `json:"cooldown,omitempty" yaml:"cooldown,omitempty" mapstructure:"cooldown"`
	MaxCooldown time.Duration `json:"maxCooldown,omitempty" yaml:"maxCooldown,omitempty" mapstructure:"maxCooldown"`
}

var (
	configuredAccounts   []Account
	configuredInterfaces []InterfaceConfig
	globalAccountPool    *AccountPool
	poolOnce             sync.Once
)

var execPath = getCurrentAbPath()
var _, filenameWithSuffix = filepath.Split(execPath)
var sysType = runtime.GOOS
var homeDir string
var homeError error

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   filenameWithSuffix,
	Short: "A program used to implement Ruijie web authentication",
	Long:  `HustWebAuth is a program used to implement Ruijie web authentication.`,
}

func parseAccountList(accStr, pwdStr, svcType string, isEncrypt bool) []Account {
	accList := strings.Split(accStr, ",")
	pwdList := strings.Split(pwdStr, ",")
	var list []Account
	for i, a := range accList {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		p := ""
		if i < len(pwdList) {
			p = strings.TrimSpace(pwdList[i])
		} else if len(pwdList) > 0 {
			p = strings.TrimSpace(pwdList[len(pwdList)-1])
		}
		enc := isEncrypt
		list = append(list, Account{
			Account:     a,
			Password:    p,
			ServiceType: svcType,
			Encrypt:     &enc,
		})
	}
	return list
}

func getEffectiveAccounts() []Account {
	// 1. If CLI flag -a / --account was explicitly provided, it takes highest precedence
	if rootCmd.PersistentFlags().Lookup("account").Changed && account != "" {
		return parseAccountList(account, password, serviceType, encrypt)
	}

	// 2. Next precedence: accounts configured in YAML (auth.accounts takes priority over auth.account)
	if len(configuredAccounts) > 0 {
		return configuredAccounts
	}

	// 3. Fallback: single account loaded from config or flag
	if account != "" {
		return parseAccountList(account, password, serviceType, encrypt)
	}

	return nil
}

func getDefaultAccountPool() *AccountPool {
	poolOnce.Do(func() {
		accounts := getEffectiveAccounts()
		if len(accounts) == 0 && iface != "" {
			for _, ifc := range configuredInterfaces {
				if ifc.Iface == iface && len(ifc.Accounts) > 0 {
					accounts = ifc.Accounts
					break
				}
			}
		}
		globalAccountPool = NewAccountPool(accounts, cooldown, maxCooldown)
	})
	return globalAccountPool
}

func runDaemon() {
	if saveCfg {
		return
	}
	if sysType != "windows" && daemonEnable {
		if logFile == "" {
			tmpDir := filepath.Join(getTmpDir(), "HustWebAuth")
			if _, err := os.Stat(tmpDir); os.IsNotExist(err) {
				os.Mkdir(tmpDir, fs.ModeDir)
			}
			logSuffix := filenameWithSuffix
			if iface != "" {
				logSuffix += "_" + iface
			}
			logFile = filepath.Join(tmpDir, logSuffix+".log")
		}
		if daemonPidFile == "" {
			pidSuffix := filenameWithSuffix
			if iface != "" {
				pidSuffix += "_" + iface
			}
			daemonPidFile = "/var/run/" + pidSuffix + "_daemon.pid"
		}
		cntxt := &daemon.Context{
			PidFileName: daemonPidFile,
			PidFilePerm: 0644,
			LogFileName: logFile,
			LogFilePerm: 0644,
		}

		child, err := cntxt.Reborn()
		if err != nil {
			log.Fatal("Unable to run: ", err)
		}
		if child != nil {
			return
		}
		defer func() {
			cntxt.Release()
			log.Println("HustWebAuth Daemon stopped.")
		}()

		log.Println("- - - - - - - - - - - - - - - - - - -")
		log.Println("HustWebAuth Daemon started.")
	}

	runCycle()
}

func runCycle() {
	log.Println("- - - - - - - - - - - - - - - - - - -")
	log.Println("HustWebAuth started.")

	// Multi-interface concurrent mode: If multiple interfaces configured in YAML and no specific -i flag passed
	if iface == "" && len(configuredInterfaces) > 0 {
		log.Printf("Starting multi-interface concurrent mode for %d interfaces...\n", len(configuredInterfaces))
		var wg sync.WaitGroup
		for _, ifcConfig := range configuredInterfaces {
			wg.Add(1)
			go func(cfg InterfaceConfig) {
				defer wg.Done()
				runSingleWorker(cfg)
			}(ifcConfig)
		}
		wg.Wait()
		return
	}

	// Single interface worker mode
	defaultCfg := InterfaceConfig{
		Iface:       iface,
		Accounts:    getEffectiveAccounts(),
		Cooldown:    cooldown,
		MaxCooldown: maxCooldown,
	}
	if iface != "" {
		for _, ifc := range configuredInterfaces {
			if ifc.Iface == iface {
				defaultCfg.PingIP = ifc.PingIP
				if len(ifc.Accounts) > 0 {
					defaultCfg.Accounts = ifc.Accounts
				}
				if ifc.Cooldown > 0 {
					defaultCfg.Cooldown = ifc.Cooldown
				}
				if ifc.MaxCooldown > 0 {
					defaultCfg.MaxCooldown = ifc.MaxCooldown
				}
				break
			}
		}
	}
	runSingleWorker(defaultCfg)
}

func runSingleWorker(cfg InterfaceConfig) {
	tag := ifaceTag(cfg.Iface)
	pool := NewAccountPool(cfg.Accounts, cfg.Cooldown, cfg.MaxCooldown)

	log.Printf("[%s] Worker initialized with %d account(s), base cooldown: %s, max cooldown: %s\n",
		tag, pool.AccountsCount(), cfg.Cooldown, cfg.MaxCooldown)

	retryCount := 0
	res, err := LoginWithInterface(cfg.Iface, pool, register)
	if err != nil {
		if cycleEnable {
			if cycleRetry < 0 {
				log.Printf("[%s] Login failed, Err: %v\n", tag, err)
				log.Printf("[%s] Login retrying...\n", tag)
			} else if retryCount < cycleRetry {
				retryCount++
				log.Printf("[%s] Login failed, Err: %v\n", tag, err)
				log.Printf("[%s] Login retry %d times after %s\n", tag, retryCount, cycleDuration)
			} else {
				log.Printf("[%s] Login failed, Err: %v\n", tag, err)
				log.Printf("[%s] Exceed the maximum number of retries, worker stopped!\n", tag)
				if len(configuredInterfaces) == 0 {
					os.Exit(1)
				}
				return
			}
		} else {
			log.Printf("[%s] Login failed, Err: %v\n", tag, err)
			if len(configuredInterfaces) == 0 {
				os.Exit(1)
			}
			return
		}
	}
	if res != "" {
		log.Printf("[%s] %s\n", tag, res)
	}

	if cycleEnable {
		eventsTick := time.NewTicker(cycleDuration)
		defer eventsTick.Stop()
		for range eventsTick.C {
			res, err := LoginWithInterface(cfg.Iface, pool, false)
			if err != nil {
				if cycleRetry < 0 {
					log.Printf("[%s] Login failed, Err: %v\n", tag, err)
					log.Printf("[%s] Login retrying...\n", tag)
				} else if retryCount < cycleRetry {
					retryCount++
					log.Printf("[%s] Login failed, Err: %v\n", tag, err)
					log.Printf("[%s] Login retry %d times after %s\n", tag, retryCount, cycleDuration)
				} else {
					log.Printf("[%s] Login failed, Err: %v\n", tag, err)
					log.Printf("[%s] Exceed the maximum number of retries, worker stopped!\n", tag)
					return
				}
			} else {
				if res != "" {
					log.Printf("[%s] %s\n", tag, res)
				}
				retryCount = 0
			}
		}
	}
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initHomeDir)
	cobra.OnInitialize(initConfig)
	cobra.OnInitialize(initLog)
	cobra.OnFinalize(saveConfig)

	rootCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		// Validate that at least one account or interface is configured
		accounts := getEffectiveAccounts()
		if len(accounts) == 0 && len(configuredInterfaces) == 0 {
			return fmt.Errorf("no authentication account configured; specify via -a/--account or config file")
		}
		return nil
	}
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		runDaemon()
	}

	rootCmd.PersistentFlags().StringVarP(&cfgFile, "config", "f", "", "Config file (default is $HOME/HustWebAuth.yaml)")
	rootCmd.PersistentFlags().StringVarP(&account, "account", "a", "", "Account(s) for authentication (comma-separated for multi-account)")
	rootCmd.PersistentFlags().StringVarP(&password, "password", "p", "", "Password(s) for authentication (comma-separated)")
	rootCmd.PersistentFlags().StringVarP(&serviceType, "serviceType", "s", "internet", "Service type, options: [internet, local]")
	rootCmd.PersistentFlags().BoolVarP(&encrypt, "encrypt", "e", false, "Password is encrypted or not (default false)")
	rootCmd.PersistentFlags().StringVarP(&iface, "iface", "i", "", "Network interface or IP address to bind (e.g. eth0, vwan1, 10.0.0.2)")
	rootCmd.PersistentFlags().DurationVar(&cooldown, "cooldown", 10*time.Minute, "Base cooldown duration for exponential backoff")
	rootCmd.PersistentFlags().DurationVar(&maxCooldown, "maxCooldown", 2*time.Hour, "Max cooldown duration for exponential backoff")
	rootCmd.PersistentFlags().BoolVar(&rotationEnable, "rotation", true, "Enable multi-account rotation")

	rootCmd.PersistentFlags().StringVar(&pingIP, "pingIP", "202.114.0.131", "IP address to ping")
	rootCmd.PersistentFlags().IntVar(&pingCount, "pingCount", 3, "ping count")
	rootCmd.PersistentFlags().DurationVar(&pingTimeout, "pingTimeout", 3*time.Second, "Ping timeout")
	rootCmd.PersistentFlags().BoolVar(&pingPrivilege, "pingPrivilege", true, `Sets the type of ping pinger will send.
false means pinger will send an "unprivileged" UDP ping.
true means pinger will send a "privileged" raw ICMP ping.
NOTE: setting to true requires that it be run with super-user privileges.
`)
	rootCmd.PersistentFlags().StringVar(&redirectURL, "redirectURL", "http://123.123.123.123", "Redirect URL")
	rootCmd.PersistentFlags().StringVar(&logDir, "logDir", filepath.Join(os.TempDir(), "HustWebAuth"), "Log Directory")
	rootCmd.PersistentFlags().StringVarP(&logFile, "logFile", "l", "", "Log file name (default means output to os.stdout)")
	rootCmd.PersistentFlags().BoolVar(&logRandom, "logRandom", true, "Log file name with random string.\nNOTE: If logFile includes a \"*\", the random string replaces the last \"*\".\n")
	rootCmd.PersistentFlags().BoolVar(&logAppend, "logAppend", true, "Log file append mode. \nNOTE: if logRandom is true, it will be ignored")
	rootCmd.PersistentFlags().BoolVar(&logConnected, "logConnected", true, "Enable logging of \"The network is connected\"")
	rootCmd.PersistentFlags().BoolVar(&sysLog, "syslog", false, "Enable syslog, not support windows")
	rootCmd.PersistentFlags().BoolVarP(&saveCfg, "save", "o", false, "Save config file")
	rootCmd.Flags().BoolVarP(&daemonEnable, "daemon", "d", false, "Enable daemon mode, not support windows")
	rootCmd.Flags().StringVar(&daemonPidFile, "daemonPidFile", "", "Daemon pid file")
	rootCmd.Flags().BoolVarP(&cycleEnable, "cycle", "c", false, "Enable cycle mode")
	rootCmd.Flags().DurationVar(&cycleDuration, "cycleDuration", 5*time.Minute, "Cycle duration")
	rootCmd.Flags().IntVar(&cycleRetry, "cycleRetry", 3, "Cycle retry times, -1 means retry forever")

	viper.BindPFlag("net.iface", rootCmd.PersistentFlags().Lookup("iface"))
	viper.BindPFlag("auth.account", rootCmd.PersistentFlags().Lookup("account"))
	viper.BindPFlag("auth.password", rootCmd.PersistentFlags().Lookup("password"))
	viper.BindPFlag("auth.serviceType", rootCmd.PersistentFlags().Lookup("serviceType"))
	viper.BindPFlag("auth.encrypt", rootCmd.PersistentFlags().Lookup("encrypt"))
	viper.BindPFlag("auth.cooldown", rootCmd.PersistentFlags().Lookup("cooldown"))
	viper.BindPFlag("auth.maxCooldown", rootCmd.PersistentFlags().Lookup("maxCooldown"))
	viper.BindPFlag("auth.rotation", rootCmd.PersistentFlags().Lookup("rotation"))

	viper.BindPFlag("ping.ip", rootCmd.PersistentFlags().Lookup("pingIP"))
	viper.BindPFlag("ping.count", rootCmd.PersistentFlags().Lookup("pingCount"))
	viper.BindPFlag("ping.timeout", rootCmd.PersistentFlags().Lookup("pingTimeout"))
	viper.BindPFlag("ping.privilege", rootCmd.PersistentFlags().Lookup("pingPrivilege"))
	viper.BindPFlag("redirect.url", rootCmd.PersistentFlags().Lookup("redirectURL"))
	viper.BindPFlag("log.dir", rootCmd.PersistentFlags().Lookup("logDir"))
	viper.BindPFlag("log.file", rootCmd.PersistentFlags().Lookup("logFile"))
	viper.BindPFlag("log.random", rootCmd.PersistentFlags().Lookup("logRandom"))
	viper.BindPFlag("log.append", rootCmd.PersistentFlags().Lookup("logAppend"))
	viper.BindPFlag("log.connected", rootCmd.PersistentFlags().Lookup("logConnected"))
	viper.BindPFlag("log.syslog", rootCmd.PersistentFlags().Lookup("syslog"))
	viper.BindPFlag("daemon.enable", rootCmd.Flags().Lookup("daemon"))
	viper.BindPFlag("daemon.pidFile", rootCmd.Flags().Lookup("daemonPidFile"))
	viper.BindPFlag("cycle.enable", rootCmd.Flags().Lookup("cycle"))
	viper.BindPFlag("cycle.duration", rootCmd.Flags().Lookup("cycleDuration"))
	viper.BindPFlag("cycle.retry", rootCmd.Flags().Lookup("cycleRetry"))

	rootCmd.CompletionOptions.HiddenDefaultCmd = true
}

func initHomeDir() {
	homeDir, homeError = os.UserHomeDir()
	if homeError != nil {
		log.Println("UserHomeDir Error:", homeError)
		homeDir = getCurrentAbDir()
		log.Println("Using Exec Dir as HOME: ", homeDir)
	}
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		viper.AddConfigPath(homeDir)
		cfgFile = filepath.Join(homeDir, "HustWebAuth.yaml")
		viper.SetConfigType("yaml")
		viper.SetConfigName("HustWebAuth")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		log.Println("Using config file: " + viper.ConfigFileUsed())

		if viper.IsSet("net.iface") && iface == "" {
			iface = viper.GetString("net.iface")
		}

		if account == "" {
			account = viper.GetString("auth.account")
		}
		if password == "" {
			password = viper.GetString("auth.password")
		}
		if !rootCmd.PersistentFlags().Lookup("serviceType").Changed && viper.IsSet("auth.serviceType") {
			serviceType = viper.GetString("auth.serviceType")
		}
		if !rootCmd.PersistentFlags().Lookup("encrypt").Changed && viper.IsSet("auth.encrypt") {
			encrypt = viper.GetBool("auth.encrypt")
		}
		if !rootCmd.PersistentFlags().Lookup("cooldown").Changed && viper.IsSet("auth.cooldown") {
			cooldown = viper.GetDuration("auth.cooldown")
		}
		if !rootCmd.PersistentFlags().Lookup("maxCooldown").Changed && viper.IsSet("auth.maxCooldown") {
			maxCooldown = viper.GetDuration("auth.maxCooldown")
		}
		if !rootCmd.PersistentFlags().Lookup("rotation").Changed && viper.IsSet("auth.rotation") {
			rotationEnable = viper.GetBool("auth.rotation")
		}

		// Load multi-accounts list from auth.accounts
		if viper.IsSet("auth.accounts") {
			var accs []Account
			if err := viper.UnmarshalKey("auth.accounts", &accs); err == nil {
				configuredAccounts = accs
			}
		}

		// If no accounts array was loaded but single account exists in YAML, create slice
		if len(configuredAccounts) == 0 && account != "" {
			enc := encrypt
			configuredAccounts = []Account{
				{
					Account:     account,
					Password:    password,
					ServiceType: serviceType,
					Encrypt:     &enc,
				},
			}
		}

		// Load multi-interfaces list from interfaces
		if viper.IsSet("interfaces") {
			var ifcs []InterfaceConfig
			if err := viper.UnmarshalKey("interfaces", &ifcs); err == nil {
				for i := range ifcs {
					if ifcs[i].Cooldown <= 0 {
						ifcs[i].Cooldown = cooldown
					}
					if ifcs[i].MaxCooldown <= 0 {
						ifcs[i].MaxCooldown = maxCooldown
					}
					if len(ifcs[i].Accounts) == 0 {
						ifcs[i].Accounts = configuredAccounts
					}
				}
				configuredInterfaces = ifcs
			}
		}

		pingIP = viper.GetString("ping.ip")
		pingCount = viper.GetInt("ping.count")
		pingTimeout = viper.GetDuration("ping.timeout")
		pingPrivilege = viper.GetBool("ping.privilege")
		redirectURL = viper.GetString("redirect.url")
		logDir = viper.GetString("log.dir")
		logFile = viper.GetString("log.file")
		logRandom = viper.GetBool("log.random")
		logAppend = viper.GetBool("log.append")
		logConnected = viper.GetBool("log.connected")
		sysLog = viper.GetBool("log.syslog")
		daemonEnable = viper.GetBool("daemon.enable")
		daemonPidFile = viper.GetString("daemon.pidFile")
		cycleEnable = viper.GetBool("cycle.enable")
		cycleDuration = viper.GetDuration("cycle.duration")
		cycleRetry = viper.GetInt("cycle.retry")
	}
}

func saveConfig() {
	if saveCfg {
		err := viper.WriteConfigAs(cfgFile)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("Save config file: " + cfgFile)
	}
}
