package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
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
		log.Fatal("no container engine detected")
	})
}

func main() {
	initContainerEngine()

	var rootCmd = &cobra.Command{
		Use:   "terasky-insights",
		Short: "Tool for TeraSky insights Assessments",
	}
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug mode")

	runFlags := RunCommandFlags{}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a container with AWS profile, IAM role and Assessment Package",
		Run:   func(cmd *cobra.Command, args []string) { runContainer(cmd, args, runFlags) },
	}

	// Add flags
	runCmd.Flags().StringVar(&runFlags.ProfileName, "profile", "", "AWS local profile name")
	runCmd.Flags().StringVar(&runFlags.ModName, "package", "", "Assessment Package to use")
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

	stopTeraSkyInsightsContianer()

	// Get the current user's home directory
	currentUser, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	homeDir := currentUser.HomeDir

	// todo support env variable to get AWS credentials
	fmt.Println("Downloading image and run ")
	execCommand(fmt.Sprintf("run -d -p 9193:9193 -p 9194:9194 -v %s/.aws:/tmp/aws:ro "+
		"--name terasky-insights --pull always --entrypoint /usr/local/bin/entrypoint.sh ghcr.io/elad-ts/terasky-insights:latest %s %s",
		homeDir,
		flags.ProfileName,
		flags.IamRole))

	// Wait for container readiness
	ready := waitForContainerReadiness()
	if !ready {
		fmt.Println("Container did not become ready in time")
		return
	}

	loadModDashbaord(flags.ModName)
}

func loadModDashbaord(modName string) {
	fmt.Println("Running Assessments")

	execCommandWithRetry(fmt.Sprintf(
		"exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
			"steampipe service stop --force && "+
			"find /tmp -type f -name \".s.PGSQL.*.lock\" -exec rm {} \\; && "+
			"steampipe service start --dashboard'", modName))

	execCommand(fmt.Sprintf("exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
		"steampipe check all --output csv > /mods/%s.csv ; exit 0'", modName, modName))

	execCommand(fmt.Sprintf("cp terasky-insights:/mods/%s.csv ./%s.csv", modName, modName))

	dir, _ := os.Getwd()
	fmt.Printf("Report Exported:  %s/%s.csv\n", dir, modName)
	fmt.Println("Report Dashboard:  http://localhost:9194")
}

func stopTeraSkyInsightsContianer() {
	containerId := strings.TrimSpace(execCommand("ps -a -q --filter name=terasky-insights"))
	if containerId != "" {
		execCommand(fmt.Sprintf("rm -f -v %s", strings.TrimSpace(containerId)))
	}
	fmt.Println("Stopping and Deleting terasky-insights")
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and delete terasky-insights",
	Args:  cobra.ExactArgs(0),
	Run:   stopContainer,
}

var packageCmd = &cobra.Command{
	Use:   "package [package_name]",
	Short: "Load a package ,Allowed values are: aws-finops, aws-top-10, aws-well-architected",
	Args:  cobra.MinimumNArgs(1),
	Run:   loadPackage,
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "version info",
	Args:  cobra.ExactArgs(0),
	Run:   getVersionInfo,
}

func execCommand(command string) string {
	return execCommandInternal(command, false)
}

func execCommandWithRetry(command string) string {
	return execCommandInternal(command, true)
}

// execCommand executes a single command using the specified runtime.
// The `containerEngine` and `command` are parameters to this function.
func execCommandInternal(command string, retry bool) string {
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
		execCommandInternal(command, false)
		containerLogsCmd := exec.Command(shell, shellOption, fmt.Sprintf("%s %s", containerEngine, "logs terasky-insights"))
		containerLogs, err := containerLogsCmd.CombinedOutput()
		if err != nil {
			log.Fatal("Please make sure your container daeamon is running")
		}
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

func waitForContainerReadiness() bool {
	maxAttempts := 30
	attempt := 0

	for attempt < maxAttempts {
		// Use a shell to check if /tmp/ready exists inside the container
		results := execCommand("exec terasky-insights /bin/sh -c 'test -f /tmp/ready && echo 1 || echo 0'")
		if results == "1" {
			return true
		}
		time.Sleep(2 * time.Second) // wait for 2 seconds before retrying
		attempt++
	}

	return false
}
