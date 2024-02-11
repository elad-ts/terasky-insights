package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// Global flags
var (
	debug           bool
	version         = "dev" // default version, overridden at build time
	containerEngine string
	once            sync.Once
)

type RunCommandFlags struct {
	ProfileName string
	IamRole     string
	ModName     string
}

type Spinner struct {
	active    bool
	stopChan  chan struct{}
	character []string
}

func NewSpinner() *Spinner {
	return &Spinner{
		character: []string{"|", "/", "-", "\\"},
		stopChan:  make(chan struct{}, 1),
	}
}

func initContainerEngine() {
	once.Do(func() {
		engines := []string{"docker", "podman", "containerd", "runc"}
		for _, engine := range engines {
			if _, err := exec.LookPath(engine); err == nil {
				containerEngine = engine
				return
			}
		}
		panic("no container engine detected")
	})
}

func main() {
	initContainerEngine()

	var rootCmd = &cobra.Command{
		Use:   "terasky-insights",
		Short: "Tool for TeraSky insights Inspections",
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	runFlags := RunCommandFlags{}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a container with AWS profile, IAM role and Inspection Package",
		Run:   func(cmd *cobra.Command, args []string) { runContainer(cmd, args, runFlags) },
	}

	// Add flags
	runCmd.Flags().StringVar(&runFlags.ProfileName, "profile", "", "AWS local profile name")
	runCmd.Flags().StringVar(&runFlags.ModName, "package", "", "Inspection Package to use")
	runCmd.Flags().StringVar(&runFlags.IamRole, "role", "", "IAM role to use")
	runCmd.MarkFlagRequired("profile")
	runCmd.MarkFlagRequired("package")

	rootCmd.AddCommand(runCmd, stopCmd, packageCmd, versionCmd)

	cobra.CheckErr(rootCmd.Execute())
}

// ValidatePackageValue checks if the provided package value matches a subdirectory name within /mods.
func ValidatePackageValue(cmd *cobra.Command, packageValue string) {
	// Validate the option
	switch packageValue {
	case "aws-finops", "aws-top-10", "aws-well-architected":
		// this option is valid, so do nothing
	default:
		log.Fatalf("Invalid option provided: %s. Allowed values are: aws-finops, aws-top-10, aws-well-architected\n", packageValue)
	}
}

func runContainer(cmd *cobra.Command, args []string, flags RunCommandFlags) {
	ValidatePackageValue(cmd, flags.ModName)

	spinner := NewSpinner()
	spinner.Start()

	defer spinner.Stop()

	err := stopTeraSkyInsightsContianer()
	if err != nil {
		fmt.Printf("Error stopping existing container: %v\n", err)
		return
	}
	// todo support IAM Access Role + Env variable to get AWS credentials
	fmt.Println("Downloading Image")
	execCommand(fmt.Sprintf("run -d -p 9193:9193 -p 9194:9194 -v ~/.aws:/tmp/aws:ro "+
		"--name terasky-insights --pull always --entrypoint /usr/local/bin/entrypoint.sh ghcr.io/elad-ts/terasky-insights:latest %s %s", flags.ProfileName,
		flags.IamRole))

	// todo - fix ugly hack to wait for container to start
	time.Sleep(10 * time.Second)
	loadModDashbaord(flags.ModName)
}

func loadModDashbaord(modName string) {
	fmt.Println("Run Inspection")

	execCommand(
		fmt.Sprintf(
			"exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
				"steampipe service stop && steampipe service start --dashboard'", modName))

	execCommand(fmt.Sprintf("exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
		"steampipe check all --output csv > /mods/%s.csv ; exit 0'", modName, modName))

	execCommand(fmt.Sprintf("cp terasky-insights:/mods/%s.csv ./%s.csv", modName, modName))

	dir, _ := os.Getwd()
	fmt.Printf("report export at %s/%s.csv\n", dir, modName)
	fmt.Println("report details at http://localhost:9194")
}

func stopTeraSkyInsightsContianer() error {
	containerId := strings.TrimSpace(execCommand("ps -a -q --filter name=terasky-insights"))
	if containerId != "" {
		execCommand(fmt.Sprintf("rm -f -v %s", strings.TrimSpace(containerId)))
	}
	return nil
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and delete terasky-insights",
	Args:  cobra.ExactArgs(0),
	Run:   stopContainer,
}

var packageCmd = &cobra.Command{
	Use:   "package [package_name]",
	Short: "Load a package",
	Args:  cobra.MinimumNArgs(1),
	Run:   loadPackage,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version info",
	Args:  cobra.ExactArgs(0),
	Run:   getVersionInfo,
}

// execCommand executes a single command using the specified runtime.
// The `containerEngine` and `command` are parameters to this function.
func execCommand(command string) string {
	// Determine the shell and shell option based on the operating system.
	var shell, shellOption string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellOption = "/C"
	} else {
		shell = "/bin/sh"
		shellOption = "-c"
	}

	// Construct the command safely, without concatenating strings directly.
	fullCommand := fmt.Sprintf("%s %s", containerEngine, command)

	if debug {
		fmt.Printf("Executing command: %s\n", fullCommand)
	}

	cmd := exec.Command(shell, shellOption, fullCommand)
	cmdOutput, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(cmdOutput))

	if debug {
		fmt.Printf("Command output: %s\n", output)
	}

	if err != nil {
		containerLogsCmd := exec.Command(shell, shellOption, fmt.Sprintf("%s %s", containerEngine, "logs terasky-insights"))
		containerLogs, _ := containerLogsCmd.CombinedOutput()
		log.Fatalf("container logs %s", string(containerLogs))
	}

	return output
}

func stopContainer(cmd *cobra.Command, args []string) {
	stopTeraSkyInsightsContianer()
}

// create loadPackage cobra func
func loadPackage(cmd *cobra.Command, args []string) {
	spinner := NewSpinner()
	spinner.Start()

	defer spinner.Stop()

	packageValue := args[0]
	ValidatePackageValue(cmd, packageValue)
	loadModDashbaord(packageValue)
}

func getVersionInfo(cmd *cobra.Command, args []string) {
	fmt.Printf("Version: %s\n", version)
}

func (s *Spinner) Start() {
	s.active = true
	go func() {
		for {
			for _, c := range s.character {
				if !s.active {
					return
				}
				fmt.Printf("\r%s Please wait...", c)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop() {
	s.active = false
	s.stopChan <- struct{}{}
}
