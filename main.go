package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"google.golang.org/api/option"
	"google.golang.org/api/youtube/v3"
)

type YouTubeResponse struct {
	Items []struct {
		ID struct {
			ChannelID string `json:"channelId"`
		} `json:"id"`
	} `json:"items"`
}

type twitchChannels struct {
	Data []twitchChannel `json:"data"`
}

type twitchTimeStruct struct {
	time.Time
}

type twitchChannel struct {
	BroadcasterLogin string `json:"broadcaster_login"`
	DisplayName string `json:"display_name"`
	GameName string `json:"game_name"`
	IsLive bool `json:"is_live"`
	Title string `json:"title"`
	StreamStartTime *twitchTimeStruct `json:"started_at"`
}

func (t *twitchTimeStruct) UnmarshalJSON(b []byte) error  {
	str := string(b)

	if str == "null" {
		return nil
	}

	str = str[1: len(str) - 1]

	if str == "" {
		return nil
	}

	parsedTime, err := time.Parse(time.RFC3339, str)
	if err != nil{
		return err
	}

	t.Time = parsedTime
	return nil
}

func makeYTCall(ChannelName string, channelID string, wg *sync.WaitGroup, mu *sync.Mutex, results chan <- map[string]string) {
	defer wg.Done()
	mu.Lock()
	defer mu.Unlock()
	ytApiKey := os.Getenv("ytApiKey")

	ctx := context.Background()
	service, err := youtube.NewService(ctx, option.WithAPIKey(ytApiKey))
	if err != nil {
		log.Fatalf("Error creating YouTube service: %v", err)
	}

	// Search for live videos from a specific channel
	call := service.Search.List([]string{"id", "snippet"}).
		ChannelId(channelID).
		EventType("live"). // Filters only live videos
		Type("video").
		MaxResults(5)

	response, err := call.Do()
	if err != nil {
		log.Fatalf("API request error: %v", err)
	}

	// Print live video results
	
	for _, item := range response.Items {
		results <- map[string]string{ChannelName: item.Id.VideoId}
		return
	}
}

func clearConsole() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "cls")
	} else {
		cmd  = exec.Command("clear")
	}

	cmd.Stdout = os.Stdout
	cmd.Run()
}

func resolveChannelIDtoStreamLink(ChannelName string, channelID string) string {
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan map[string]string, 1)

	wg.Add(1)
	go makeYTCall(ChannelName, channelID, &wg, &mu, results)
	
	go func() {
		wg.Wait()
		close(results)
	}()

	result := <-results

	return result[ChannelName]
	
}

func watchChannelArray(channels [][]string) {
	var cname string
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Who would you like to watch: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}

	var id string

	for _, channel := range channels {
		if cname == channel[0] {
			id = channel[1]
		}
	}

	stream_link := resolveChannelIDtoStreamLink(cname, id)

	if len(stream_link) == 0 {
		log.Fatal("Streamer is not live\nExitting Program!")
	}

	cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=" + stream_link)
	cmd.Start()
	clearConsole()

	fmt.Println("MPV Loading...")
}

func addYoutube(){
	var cname string
	ytApiKey := os.Getenv("ytApiKey")
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Add Channel: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}


	apiURL := "https://www.googleapis.com/youtube/v3/search?" + url.Values{
		"part": {"snippet"},
		"q": {cname},
		"type": {"channel"},
		"key" : {ytApiKey},
	}.Encode()

	resp, err := http.Get(apiURL)
	if err != nil {
		log.Fatal("Could not look up channel", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var ytResponse YouTubeResponse
	if err := json.Unmarshal(body, &ytResponse); err != nil {
		log.Fatal(err)
	}

	if len(ytResponse.Items) == 0 {
		log.Fatal("channel not found")
	}

	file, err := os.OpenFile("channels.csv", os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		log.Fatal("Error writing to csv.",err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
 	defer writer.Flush()

	data := []string{cname,"youtube",ytResponse.Items[0].ID.ChannelID}
	
	err = writer.Write(data)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Succesfully added %s.\n", cname)
}

func openYTChannels() [][]string {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	return records

}

func checkYoutubeLive(channels [][]string) map[string]string{
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan map[string]string, len(channels))

	for _, pair := range channels {
		wg.Add(1)
		go makeYTCall(pair[0], pair[2], &wg, &mu, results)
	}
	
	go func() {
		wg.Wait()
		close(results)
	}()

	finalResults := make(map[string]string)
	
	for result := range results {
		for key, value := range result {
			finalResults[key] = value
		}
	}

	return finalResults
}

func parseTwitchChannelData(ChannelName string, unparsedData []byte) (twitchChannel, bool) {
	var replyDataField twitchChannels

	err := json.Unmarshal(unparsedData, &replyDataField)
	if err != nil {
		log.Fatal("Error in unmarshalling json data", err)
	}

	for _, channel := range replyDataField.Data {
		if channel.BroadcasterLogin == ChannelName {
			return channel, true
		}
	}

	var tc twitchChannel

	return tc, false
}

func makeTwitchCall(ChannelName string, wg *sync.WaitGroup, mu *sync.Mutex, results chan <- twitchChannel)   {
	defer wg.Done()
	endpoint := "https://api.twitch.tv/helix/search/channels?query=" + ChannelName

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		log.Fatal("Something went wrong in creating request", err)
	}

	req.Header.Set("Client-ID", os.Getenv("client_id"))
	req.Header.Set("Authorization", "Bearer " + os.Getenv("oauth_token"))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("Something went wrong in sending the request to twitch", err) 
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal("Couldn't read the response body for some reason", err)
	}

	channelJSON, success := parseTwitchChannelData(ChannelName, body)
	if !success {
		return
	}

	results <- channelJSON
}

func checkTwitchLive(channels []string) {
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan twitchChannel, len(channels))

	for _, channel := range channels {
		wg.Add(1)
		go makeTwitchCall(channel, &wg, &mu, results)
	}
	
	go func() {
		wg.Wait()
		close(results)
	}()

	for channel := range results {
		if channel.IsLive {
			fmt.Printf("%s is live! Title: %s\n", channel.BroadcasterLogin, channel.Title)
		}
	}

}

func openMPV(platform string, streamURL string) {
	switch platform {
	case "youtube":
		cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=" + streamURL)
		cmd.Start()
		clearConsole()
		fmt.Println("MPV Loading...")
	case "twitch":
		cmd := exec.Command("mpv", "https://twitch.tv/" + streamURL)
		cmd.Start()
		clearConsole()
		fmt.Println("MPV Loading...")
	}
}

func checkBoth() {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	var ytDoubleArray [][]string
	var twitchArray []string

	for _, channel := range records {
		if channel[2] == "" {
			twitchArray = append(twitchArray, channel[0])
		} else {
			ytDoubleArray = append(ytDoubleArray, channel)
		}
	}

	currentlyLiveYTChannels := checkYoutubeLive(ytDoubleArray)
	fmt.Println("On Twitch: ")
	checkTwitchLive(twitchArray)

	fmt.Println("On Youtube: ")
	for key, _ := range currentlyLiveYTChannels {
		// fmt.Printf("%s = %s\n", key, value)
		fmt.Printf("%s\n", key)
	}

	fmt.Println("Who would you like to watch: ")
	var cname string
	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		cname = scanner.Text()
	}

	if streamURL, ok := currentlyLiveYTChannels[cname]; ok {
		openMPV("youtube", streamURL)
	} else {
		openMPV("twitch", cname)
	}


}

func onlyYoutube() {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	var ytDoubleArray [][]string

	for _, channel := range records {
		if channel[2] == "" {
			// twitchArray = append(twitchArray, channel[0])
		} else {
			ytDoubleArray = append(ytDoubleArray, channel)
		}
	}

	currentlyLiveYTChannels := checkYoutubeLive(ytDoubleArray)

	fmt.Println("On Youtube: ")
	for key, _ := range currentlyLiveYTChannels {
		fmt.Printf("%s\n", key)
	}

	fmt.Println("Who would you like to watch: ")
	var cname string
	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		cname = scanner.Text()
	}

	if streamURL, ok := currentlyLiveYTChannels[cname]; ok {
		openMPV("youtube", streamURL)
	} else {
		fmt.Println("Error in giving streamer name.")
	}
}

func onlyTwitch() {
	file, err := os.Open("channels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	var twitchArray []string

	for _, channel := range records {
		if channel[2] == "" {
			twitchArray = append(twitchArray, channel[0])
		}
	}

	fmt.Println("On Twitch: ")
	checkTwitchLive(twitchArray)

	fmt.Println("Who would you like to watch: ")
	var cname string
	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		cname = scanner.Text()
	}

	openMPV("twitch", cname)
}

func addTwitch() {
	file, err := os.OpenFile("channels.csv", os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		log.Fatal("Error writing to csv.",err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
 	defer writer.Flush()

	 fmt.Println("Who would you like to add: ")
	 var cname string
	 scanner := bufio.NewScanner(os.Stdin)
 
	 if scanner.Scan() {
		 cname = scanner.Text()
	 }

	data := []string{cname,"twitch",""}
	
	err = writer.Write(data)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Succesfully added %s.\n", cname)
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error in loading env. ", err)
	}

	var choice int
	for choice != 4 {
		fmt.Println("1.Check Youtube and Twitch.\n2.Check Youtube.\n3.Check Twitch.\n4.Add Twitch Channel.\n5.Add Youtube Channel.\n6.Exit.")
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			checkBoth()
		case 2:
			onlyYoutube()
		case 3:
			onlyTwitch()
		case 4:
			addTwitch()
		case 5:
			addYoutube()
		case 6:
			os.Exit(0)
		}

	}
	
}
