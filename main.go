package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// RunCommandFlags holds the flags for the run command
type RunCommandFlags struct {
	ProfileName string
	IamRole     string
	ModName     string
}

// containerEngine is a package-level variable storing the name of the detected container engine.
var containerEngine string

// once ensures that the container engine detection is performed only once.
var once sync.Once

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
	rootCmd.AddCommand(reportCmd)
	rootCmd.AddCommand(packageCmd)

	cobra.CheckErr(rootCmd.Execute())
}

// ValidatePackageValue checks if the provided package value matches a subdirectory name within /mods.
func ValidatePackageValue(cmd *cobra.Command, packageValue string) {
	// Validate the option
	switch packageValue {
	case "aws-finops", "aws-top-10", "aws-well-architected":
		fmt.Printf("Option selected: %s\n", packageValue)
	default:
		fmt.Printf("Invalid option provided: %s. Allowed values are: aws-finops, aws-top-10, aws-well-architected\n", packageValue)
		cmd.Usage() // Show usage if the option is invalid
	}
}

func runContainer(cmd *cobra.Command, args []string, flags RunCommandFlags) {

	ValidatePackageValue(cmd, flags.ModName)

	err := stopTeraSkyLabContianer()
	if err != nil {
		fmt.Printf("Error stopping existing container: %v\n", err)
		return
	}

	execCommand(fmt.Sprintf("run -d -p 9193:9193 -p 9194:9194 -v ~/.aws:/tmp/aws:ro "+
		"--name terasky-insights --entrypoint /usr/local/bin/entrypoint.sh ghcr.io/terasky-oss/terasky-insights:latest %s %s %s", flags.ProfileName,
		flags.ModName, flags.IamRole))
}

func loadModDashbaord(modName string) {
	execCommand(
		fmt.Sprintf(
			"exec terasky-insights /bin/sh -c 'cd /mods/%s && "+
				// "export AWS_PROFILE=%s "+
				"export STEAMPIPE_DATABASE_START_TIMEOUT=300 && "+
				"steampipe service stop && steampipe service start --dashboard' ",
			modName,
		),
	)
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

var reportCmd = &cobra.Command{
	Use:   "report",
	Short: "Run a report",
	Args:  cobra.ExactArgs(0),
	Run:   runReport,
}

var packageCmd = &cobra.Command{
	Use:   "package [package_name]",
	Short: "Load a package",
	Args:  cobra.MinimumNArgs(1),
	Run:   loadPackage,
}

// execCommand executes a single command using the specified runtime.
// The `containerEngine` and `command` are parameters to this function.
func execCommand(command string) string {
	fmt.Println("Executing command:", command)

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
		// Log the error and exit the program
		log.Fatalf("Error executing command '%s': %v\nOutput: %s", command, err, output)
	}

	fmt.Printf("Command '%s' executed successfully.\nOutput: %s\n", command, output)
	return output
}

func stopContainer(cmd *cobra.Command, args []string) {
	stopTeraSkyLabContianer()
}

func runReport(cmd *cobra.Command, args []string) {
	execCommand("exec terasky-insights /bin/sh -c 'cd /mods/aws-top-10 && steampipe check all --output csv > aws-top-10-report.csv'")
	execCommand("cp terasky-insights:/mods/aws-top-10/aws-top-10-report.csv ./aws-top-10-report.csv")
}

// create loadPackage cobra func
func loadPackage(cmd *cobra.Command, args []string) {
	packageValue := args[0]
	ValidatePackageValue(cmd, packageValue)

	//todo need to check if reload will requires Posgress restart as well
	loadModDashbaord(packageValue)
}
