package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ursiform/sleuth"
)

const (
	streamPort     = "7843"
	ipDiscoveryURL = "sleuth://streamplay-ip/ip:9872"
)

var (
	ipChan   chan string
	logLevel string
)

/*
	Functions to handle selection of audio device
*/

func printDev() {
	// Regexp to extract device names
	regex := regexp.MustCompile("(\"[A-z].*?\")")

	// Pull input devices from ffmpeg
	cmd := exec.Command("ffmpeg", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	out, _ := cmd.CombinedOutput()

	// Split audio and video devices: video will be outSl[0] and audio will be outSl[1]
	outSl := strings.Split(string(out), "DirectShow audio devices")

	// Insert newline after each device
	v := strings.Join(regex.FindAllString(fmt.Sprintf("%s", outSl[0]), -1), "\n")
	a := strings.Join(regex.FindAllString(fmt.Sprintf("%s", outSl[1]), -1), "\n")

	// Print out video devices followed by audio devices
	fmt.Printf("Available video devices (may support audio as well):\n%s\n\n"+
		"Available audio devices:\n%s", v, a)
}

/*
	Functions to handle autodiscovery of service on local network
*/

func autodiscover(iface string) {
	config := &sleuth.Config{
		Interface: iface,
		LogLevel:  logLevel,
	}

	client, err := sleuth.New(config)
	defer client.Close()
	if err != nil {
		fmt.Print("Error initializing sleuth client: ", err)
	}

	for {
		// Wait for server to come online
		client.WaitFor("streamplay-ip")

		// Wait for server to finish coming online
		time.Sleep(time.Second)
		req, err := http.NewRequest("GET", ipDiscoveryURL, nil)
		if err != nil {
			fmt.Print("Error forming GET request to client: ", err)

			continue
		}

		// Request IP from client
		res, err := client.Do(req)
		if err != nil {
			fmt.Print("Error getting IP from client: ", err)
			time.Sleep(time.Second) // Wait for client to disconnect
			continue
		}

		// Read IP from response
		ip, err := ioutil.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			fmt.Print("Error reading IP from client: ", err)
			continue
		}

		// Send IP to stream func
		ipChan <- string(ip)

		// Sleep before repeating
		time.Sleep(time.Second)
	}
}

/*
	Functions to implement streaming with ffmpeg
*/

func streamAudio(aSrc string) {
	for ip := range ipChan {
		fmt.Printf("Streaming to %s:%s\n", ip, streamPort)

		// Program args for ffmpeg
		args := []string{
			// 'dshow' us used for windows only
			"-f", "dshow",

			// Inputs
			"-i", fmt.Sprintf("audio=%s", aSrc),

			// Audio options
			"-acodec", "libmp3lame", "-ab", "128k", "-ar", "44100",

			// Output options
			"-maxrate", "1m", "-bufsize", "3000k", "-f", "rtsp", "-rtsp_transport", "tcp",
			fmt.Sprintf("rtsp://%s:%s", ip, streamPort),
		}

		stream := exec.Command("ffmpeg", args...)
		stream.Stdout = os.Stdout
		stream.Stderr = os.Stderr

		err := stream.Start()
		if err != nil {
			fmt.Print(err)
		}
	}
}

func stream(aSrc, vSrc string) {
	for ip := range ipChan {
		fmt.Printf("Streaming to %s:%s\n", ip, streamPort)

		if aSrc == "" {
			// Duplicate video source for audio
			aSrc = vSrc
		}

		// Program args for ffmpeg
		args := []string{
			// 'dshow' us used for windows only
			"-f", "dshow",

			// Inputs
			"-i", fmt.Sprintf("video='%s':audio='%s'", vSrc, aSrc),

			// Video options
			"-preset", "ultrafast", "-vcodec", "libx264", "-tune", "zerolatency",
			"-r", "24", "-async", "1",

			// Audio options
			"-acodec", "libmp3lame", "-ab", "128k", "-ar", "44100",

			// Output options
			"-maxrate", "1m", "-bufsize", "3000k", "-f", "rtsp", "-rtsp_transport", "tcp",
			fmt.Sprintf("rtsp://%s:%s/live.sdp", ip, streamPort),
		}

		stream := exec.Command("ffmpeg", args...)
		stream.Stdout = os.Stdout
		stream.Stderr = os.Stderr

		err := stream.Start()
		if err != nil {
			fmt.Print(err)
		}
	}
}

// main starts the autodiscovery server, parses flags, and begins streaming
func main() {
	var (
		listDev           bool
		aSrc, vSrc, iface string
	)

	flag.BoolVar(&listDev, "dev", false, "Lists available input devices")
	flag.StringVar(&aSrc, "a", "", "Audio device to stream")
	flag.StringVar(&vSrc, "v", "", "Video device to use")
	flag.StringVar(&iface, "iface", "Wi-Fi", "Network interface on which to listen for clients")
	flag.StringVar(&logLevel, "d", "silent", "Log level for sleuth ('debug', 'error', 'warn', or 'silent')")
	flag.Parse()

	if listDev {
		printDev()
	} else {
		ipChan = make(chan string)

		// Start autodiscovery server
		go autodiscover(iface)

		// Default to audio streaming if no video device specified
		if aSrc == "" && vSrc == "" {
			fmt.Println("You must specify an audio device or a video device to stream with the -a or -v flags")
			flag.Usage()
		} else if vSrc == "" {
			// Start streaming server with audio only
			streamAudio(aSrc)
		} else {
			// Start streaming server with video
			stream(aSrc, vSrc)
		}
	}
}
