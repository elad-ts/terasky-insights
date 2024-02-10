package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// RunCommandFlags holds the flags for the run command
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

// containerEngine is a package-level variable storing the name of the detected container engine.
var containerEngine string

// once ensures that the container engine detection is performed only once.
var once sync.Once

var version = "dev" // default version, overridden at build time

// initContainerEngine detects the container engine and stores its name in containerEngine.
// It panics if no container engine is found.
func initContainerEngine() {
	once.Do(func() {
		engines := []string{"docker", "podman", "containerd", "runc"}

		for _, engine := range engines {
			if _, err := exec.LookPath(engine); err == nil {
				containerEngine = engine
				return
			}
		}

		// Panic if no container engine is found.
		panic("no container engine detected")
	})
}

func main() {
	// Initialize the container engine. This will panic if no engine is found.
	// todo - add support for version command
	initContainerEngine()

	var rootCmd = &cobra.Command{
		Use:   "terasky-insights",
		Short: "Tool for TeraSky Lab Inspections",
	}
	runFlags := RunCommandFlags{}
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run a container with AWS profile , IAM role and Inspection Package",
		Run:   func(cmd *cobra.Command, args []string) { runContainer(cmd, args, runFlags) },
	}

	// Binding flags to the run command
	runCmd.Flags().StringVar(&runFlags.ProfileName, "profile", "", "AWS local profile name")
	runCmd.Flags().StringVar(&runFlags.ModName, "package", "", "Inspection Package to use")
	runCmd.Flags().StringVar(&runFlags.IamRole, "role", "", "IAM role to use")

	runCmd.MarkFlagRequired("profile")
	runCmd.MarkFlagRequired("package")

	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(packageCmd)
	rootCmd.AddCommand(versionCmd)

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

	err := stopTeraSkyLabContianer()
	if err != nil {
		fmt.Printf("Error stopping existing container: %v\n", err)
		return
	}

	execCommand(fmt.Sprintf("run -d -p 9193:9193 -p 9194:9194 -v ~/.aws:/tmp/aws:ro "+
		"--name terasky-insights --pull always --entrypoint /usr/local/bin/entrypoint.sh ghcr.io/elad-ts/terasky-insights:latest %s %s", flags.ProfileName,
		flags.IamRole))

	// todo - fix ugly hack to wait for container to start
	time.Sleep(10 * time.Second)
	loadModDashbaord(flags.ModName)
}

func loadModDashbaord(modName string) {
	execCommand(
		fmt.Sprintf(
			"exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
				"steampipe service stop && steampipe service start --dashboard'", modName))

	execCommand(fmt.Sprintf("exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
		"steampipe check all --output csv > /mods/%s.csv ; exit 0'", modName, modName))

	execCommand(fmt.Sprintf("cp terasky-insights:/mods/%s.csv ./%s.csv", modName, modName))
}

func stopTeraSkyLabContianer() error {
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
	cmd := exec.Command(shell, shellOption, fullCommand)

	cmdOutput, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(cmdOutput))
	if err != nil {
		containerLogsCmd := exec.Command(shell, shellOption, fmt.Sprintf("%s %s", containerEngine, "logs terasky-insights"))
		containerLogs, _ := containerLogsCmd.CombinedOutput()
		log.Fatalf("container logs %s", string(containerLogs))
	}

	return output
}

func stopContainer(cmd *cobra.Command, args []string) {
	stopTeraSkyLabContianer()
}

// create loadPackage cobra func
func loadPackage(cmd *cobra.Command, args []string) {
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
