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
	fmt.Println("Running Assessment")

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
	fmt.Println("Report Dashboard:  http://0.0.0.0:9194")
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

// execCommand executes a single command and returns the output along with any error encountered.
func execCommandOnce(command string) (string, error) {
	// Determine the shell and shell option based on the operating system.
	var shell, shellOption string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		shellOption = "/C"
	} else {
		shell = "/bin/sh"
		shellOption = "-c"
	}

	// Construct the command safely.
	fullCommand := fmt.Sprintf("%s %s", containerEngine, command)

	if debug {
		fmt.Printf("Executing command: %s\n", fullCommand)
	}

	cmd := exec.Command(shell, shellOption, fullCommand)
	cmdOutput, err := cmd.CombinedOutput()
	output := strings.TrimSpace(string(cmdOutput))

	if debug {
		fmt.Printf("Command output: %s\n", output)
		if err != nil {
			fmt.Printf("Error: %s\n", err)
		}
	}

	return output, err
}

// execCommandInternal wraps execCommand with retry logic.
func execCommandInternal(command string, retry bool) string {
	output, err := execCommandOnce(command)
	if err != nil && retry {
		// Retry once
		output, err = execCommandOnce(command)
		if err == nil {
			return "Retry successful"
		}
		// Handle failure after retry
		containerLogsCmd := fmt.Sprintf("%s logs terasky-insights", containerEngine)
		containerLogsOutput, logsErr := execCommandOnce(containerLogsCmd)
		if logsErr != nil && debug {
			fmt.Printf("Failed to get container logs: %s\n", containerLogsOutput)
		}
		printChecklist()
	}

	if err != nil {
		return fmt.Sprintf("Error: %s", err)
	}

	return output
}

// printChecklist prints a beautifully formatted checklist for the user.
func printChecklist() {
	checklist := []string{
		"Ensure your container daemon is actively running.",
		"Start Terasky Insights by executing 'run assessment'.",
		"Verify AWS credentials are properly set in the current user's ~/.aws directory.",
		"Make sure that your AWS profile is valid and not expired.",
	}

	// Print the checklist
	log.Println("📋 **Checklist for Ensuring Proper Setup** 📋")
	for i, item := range checklist {
		fmt.Printf("✅ **Step %d**: %s\n", i+1, item)
	}
	log.Fatal("Unable to run ")

}
