package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"text/tabwriter"
)

// ClusterSpec represents the parameters for eksctl,
// as cluster metadata including owner and how long the cluster
// still has to live.
type ClusterSpec struct {
	// ID is a unique identifier for the cluster
	ID string `json:"id"`
	// Name specifies the cluster name
	Name string `json:"name"`
	// NumWorkers specifies the number of worker nodes, defaults to 1
	NumWorkers int `json:"numworkers"`
	// KubeVersion  specifies the Kubernetes version to use, defaults to `1.12`
	KubeVersion string `json:"kubeversion"`
	// Timeout specifies the timeout in minutes, after which the cluster
	// is destroyed, defaults to 10
	Timeout int `json:"timeout"`
	// Timeout specifies the cluster time to live in minutes.
	// In other words: the remaining time the cluster has before it is destroyed
	TTL int `json:"ttl"`
	// Owner specifies the email address of the owner (will be notified when cluster is created and 5 min before destruction)
	Owner string `json:"owner"`
	// CreationTime is the UTC timestamp of when the cluster was created
	// which equals the point in time of the creation of the respective
	// JSON representation of the cluster spec as an object in the metadata
	// bucket
	CreationTime string `json:"created"`
	// ClusterDetails is only valid for lookup of individual clusters,
	// that is, when user does, for example, a eksp l CLUSTERID. It
	// holds info such as cluster status and config
	ClusterDetails map[string]string `json:"details"`
}

var Version string

func main() {
	if len(os.Args) <= 1 {
		pinfo(fmt.Sprintf("This is EKSphemeral in version %v", Version))
		perr("Please specify one of the following commands: install, uninstall, create, list, or prolong", nil)
		os.Exit(1)
	}
	eksphome, ok := os.LookupEnv("EKSPHEMERAL_HOME")
	if !ok {
		perr("Please set the EKSPHEMERAL_HOME environment variable ", nil)
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "install", "i":
		pinfo("Trying to install EKSphemeral ...")
		shellout(eksphome + "/eksp-up.sh")
	case "uninstall", "u":
		pinfo("Trying to uninstall EKSphemeral ...")
		shellout(eksphome + "/eksp-down.sh")
	case "create", "c":
		pinfo("Trying to create a new ephemeral cluster ...")
		if len(os.Args) > 2 {
			clusterSpecFile := os.Args[2]
			pinfo("... using cluster spec " + clusterSpecFile)
			if _, err := os.Stat(clusterSpecFile); os.IsNotExist(err) {
				perr("Can't create a cluster due to invalid spec:", err)
				os.Exit(2)
			}
			shellout(eksphome+"/eksp-create.sh", clusterSpecFile)
			break
		}
		// creating cluster with defaults:
		shellout(eksphome+"/eksp-create.sh", os.Args[2])
	case "list", "ls", "l":
		if len(os.Args) > 2 { // we have a cluster ID, try looking up cluster spec
			cID := os.Args[2]
			res := bshellout(eksphome+"/eksp-list.sh", cID)
			cs, err := parseCS(res)
			if err != nil {
				perr("Can't render cluster details. Cluster could be gone or control plane is down :(", nil)
				break
			}
			cs.ID = cID
			fmt.Println(cs)
			break
		}
		// listing all cluster:
		res := bshellout(eksphome + "/eksp-list.sh")
		listClusters(eksphome, res)
	case "prolong", "p":
		if len(os.Args) < 4 {
			perr("Can't prolong cluster lifetime without both the cluster ID and the time in minutes provided", nil)
			os.Exit(3)
		}
		cID := os.Args[2]
		prolongFor := os.Args[3]
		shellout(eksphome+"/eksp-prolong.sh", cID, prolongFor)
	default:
		perr("Please specify one of the following commands: install, uninstall, create, list, or prolong", nil)
	}
}

// shellout shells out to execute a command with a variable number
// of arguments and prints the literal results from both stdout and stderr
func shellout(command string, args ...string) {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		perr("Can't shell out due to issues with stderr:", err)
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		perr("Can't shell out due to issues with stdout:", err)
		return
	}
	err = cmd.Start()
	if err != nil {
		perr("Can't shell out due to issues with starting command:", err)
		return
	}
	go echo(stderr)
	go echo(stdout)
	err = cmd.Wait()
	if err != nil {
		perr("Something bad happened after command completed:", err)
	}
}

// bshellout shells out to execute a command with a variable number
// of arguments in a blocking manner. It returns the combined literal
// output from both stdout and stderr
func bshellout(command string, args ...string) string {
	cmd := exec.Command(command, args...)
	cmd.Env = os.Environ()
	stderr, err := cmd.StderrPipe()
	if err != nil {
		perr("Can't shell out due to issues with stderr:", err)
		return ""
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		perr("Can't shell out due to issues with stdout:", err)
		return ""
	}
	err = cmd.Start()
	if err != nil {
		perr("Can't shell out due to issues with starting command:", err)
		return ""
	}
	go echo(stderr)
	result := slurp(stdout)
	err = cmd.Wait()
	if err != nil {
		perr("Something bad happened after command completed:", err)
	}
	return result
}

// echo prints the character stream as a set of lines
func echo(rc io.ReadCloser) {
	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
}

// slurp collects the character stream into one string
func slurp(rc io.ReadCloser) string {
	var buf bytes.Buffer
	scanner := bufio.NewScanner(rc)
	for scanner.Scan() {
		buf.WriteString(scanner.Text())
	}
	return buf.String()
}

// pinfo writes msg in light blue to stderr
// see also https://misc.flogisoft.com/bash/tip_colors_and_formatting
func pinfo(msg string) {
	_, _ = fmt.Fprintf(os.Stdout, "\x1b[94m%v\x1b[0m\n", msg)
}

// perr writes message (and optionally error) in light red to stderr
// see also https://misc.flogisoft.com/bash/tip_colors_and_formatting
func perr(msg string, err error) {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "%v: \x1b[91m%v\x1b[0m\n", msg, err)
		return
	}
	_, _ = fmt.Fprintf(os.Stderr, "\x1b[91m%v\x1b[0m\n", msg)
}

func parseCS(clusterspec string) (cs ClusterSpec, err error) {
	err = json.Unmarshal([]byte(clusterspec), &cs)
	if err != nil {
		return cs, err
	}
	return cs, nil
}

func listClusters(eksphome, cIDs string) {
	cl := []string{}
	err := json.Unmarshal([]byte(cIDs), &cl)
	if err != nil {
		perr("Can't render cluster spec due to:", err)
	}
	if len(cl) == 0 {
		pinfo("No clusters found")
		return
	}

	const padding = 3
	w := tabwriter.NewWriter(os.Stdout, 0, 0, padding, ' ', 0)
	fmt.Fprintln(w, "NAME\tID\tKUBERNETES\tNUM WORKERS\tTIMEOUT\tTTL\tOWNER\t")
	for _, cID := range cl {
		res := bshellout(eksphome+"/eksp-list.sh", cID)
		cs, err := parseCS(res)
		if err != nil {
			continue
		}
		cs.ID = cID
		fmt.Fprintf(w, "%s\t%s\tv%s\t%d\t%d min\t%d min\t%s\t\n", cs.Name, cs.ID, cs.KubeVersion, cs.NumWorkers, cs.Timeout, cs.TTL, cs.Owner)
	}
	w.Flush()
}

func (cs ClusterSpec) String() string {
	if cs.Name == "" {
		return fmt.Sprintf("Cluster does not exist or control plane is down")
	}
	details := fmt.Sprintf("Status:\t\t\t%s\n\tEndpoint:\t\t%s\n\tPlatform version:\t%s\n\tVPC config:\t\t%s\n\tIAM role:\t\t%s\n",
		cs.ClusterDetails["status"], cs.ClusterDetails["endpoint"], cs.ClusterDetails["platformv"], cs.ClusterDetails["vpcconf"], cs.ClusterDetails["iamrole"],
	)

	return fmt.Sprintf(
		"ID:\t\t%s\nName:\t\t%s\nKubernetes:\tv%s\nWorker nodes:\t%d\nTimeout:\t%d min\nTTL:\t\t%d min\nOwner:\t\t%s\nDetails:\n\t%s",
		cs.ID, cs.Name, cs.KubeVersion, cs.NumWorkers, cs.Timeout, cs.TTL, cs.Owner, details,
	)
}
