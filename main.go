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
		// log.Fatalf("Error creating YouTube service: %v", err)
		log.Printf("Error creating YouTube service: %v", err)
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
		fmt.Println("Live Videos from the Channel:")
		fmt.Printf("Channel: %s, Title: %s, URL: https://www.youtube.com/watch?v=%s\n",
			ChannelName, item.Snippet.Title, item.Id.VideoId)
			 results <- map[string]string{ChannelName: item.Id.VideoId}
			 return

	}
	
	

	if len(response.Items) == 0 {
		// fmt.Printf("%s is not live.\n", ChannelName)
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

func watchChannelMap(channelMap map[string]string) {
	var cname string
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Who would you like to watch: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}

	cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=" + channelMap[cname])
	cmd.Start()
	clearConsole()

	fmt.Println("MPV Loading...")
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

func appendCSV(ChannelName string, cID string) {
	file, err := os.OpenFile("channels.csv", os.O_APPEND|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		log.Fatal("Couldn't open file to write.")
	}

	defer file.Close()

	writer := csv.NewWriter(file)

	newRecord := []string{ChannelName, cID}

	if err := writer.Write(newRecord); err != nil {
		fmt.Println("Something went wrong in writing to csv", err)
		return
	}

	writer.Flush()

	if err := writer.Error(); err != nil {
		fmt.Println("Something went wrong flushing the writer", err)
	}
}

func addChannel() ([]string, error){
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
		return []string{}, err
	}

	if len(ytResponse.Items) > 0 {
		appendCSV(cname,  ytResponse.Items[0].ID.ChannelID)
	}

	return []string{}, fmt.Errorf("channel not found")
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

func checkYoutubeLive() {
	channels := openYTChannels()

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan map[string]string, len(channels))

	for _, pair := range channels {
		wg.Add(1)
		go makeYTCall(pair[0], pair[1], &wg, &mu, results)
	}
	
	go func() {
		wg.Wait()
		close(results)
	}()

	finalResults := make(map[string]string)

	for result := range results {
		for key, value := range result {
			fmt.Printf("%s = %s\n", finalResults[key], value)
			finalResults[key] = value
		}
	}

	fmt.Println("Available Channels:")

	for key, _ := range finalResults {
		fmt.Println(key)
	}

	watchChannelMap(finalResults)

}

func watchStream() {
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

	for _, channel := range records {
		fmt.Println(channel[0])
	}

	watchChannelArray(records)
}

func openTwitchChannels() [][]string {
	file, err := os.Open("twitchchannels.csv")

	if err != nil {
		log.Fatal("Couldn't open file", err)
	}

	defer file.Close()

	reader := csv.NewReader(file)

	records, err := reader.ReadAll()
	if err != nil {
		log.Fatal("Couldn't read the entries", err)
	}

	// for _, record := range records {
	// 	fmt.Print(record[0])
	// }
	return records

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

func checkTwitchLive() {
	channels := openTwitchChannels()

	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make(chan twitchChannel, len(channels))

	for _, pair := range channels {
		wg.Add(1)
		go makeTwitchCall(pair[0], &wg, &mu, results)
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

func watchTwitchStream() {
	var cname string
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Who would you like to watch: ")

	if scanner.Scan() {
		cname = scanner.Text()
	}

	cmd := exec.Command("mpv", "https://twitch.tv/" + cname)
	cmd.Start()
	clearConsole()

	fmt.Println("MPV Loading...")


}

func checkBoth()

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error in loading env. ", err)
	}

	var choice int
	for choice != 4 {
		fmt.Println("1.Check Youtube and Twitch.\n2.Check Youtube.\n3.Check Twitch.\n4.Watch stream.\n5.Exit")
		fmt.Scanln(&choice)

		switch choice {
		case 1:
			checkBoth()
		case 2:
			checkYoutubeLive()
		case 3:
			checkTwitchLive()
		case 4:
			watchTwitchStream()
		case 5:
			os.Exit(0)
		}

	}
	// var wg sync.WaitGroup
	// wg.Add(1)
	// err := godotenv.Load()
	// if err != nil {
	// 	log.Fatal("Error loading API key. Check .env file")
	// }

	// reply := testTwitch(&wg)
	// wg.Wait()

	// // fmt.Print(string(reply))

	// var replyDataField twitchChannels

	// err = json.Unmarshal(reply, &replyDataField)
	// if err != nil {
	// 	log.Fatal("Error in unmarshalling json data", err)
	// }

	// // fmt.Print(replyDataField.Data)

	
	// for _, channel := range replyDataField.Data {
	// 	if channel.BroadcasterLogin == "k4sen" {
	// 		fmt.Print(channel)
	// 	}
	// }




	// ytApiKey := os.Getenv("ytApiKey")
	// fmt.Printf("%s\n", ytApiKey)

	// channels := openChannels()
	// for _, pair := range channels {
	// 	fmt.Printf("%s, %s\n", pair[0], pair[1])
	// }

	// cmd := exec.Command("mpv", "https://www.youtube.com/watch?v=uHZ3Qy2taIk")
	// cmd.Start()

	

	// var choice int
	// for choice != 4 {
	// 	fmt.Println("1.Add Channel.\n2.Check who is live.\n3.Watch Stream\n4.Exit")
	// 	fmt.Scanln(&choice)

	// 	switch choice {
	// 	case 1:
	// 		addChannel()
	// 	case 2:
	// 		checkLive()
	// 	case 3:
	// 		watchStream()
	// 	case 4:
	// 		os.Exit(0)
	// 	}

	// }
	
	

	

	
}
