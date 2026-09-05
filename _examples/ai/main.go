package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/syumai/workers"
	"github.com/syumai/workers/cloudflare/ai"
	"github.com/syumai/workers/cloudflare/fetch"
)

func handleError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	w.Write([]byte(msg + "\n"))
}

func main() {

	http.HandleFunc("/ai", func(w http.ResponseWriter, req *http.Request) {

		// initialize AI namespace instance
		aiCaller, err := ai.New("AI")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init AI instance: %v", err)
			return
		}

		aiJsonResultStr, err := aiCaller.Run("@cf/meta/llama-3.1-8b-instruct", map[string]any{
			"prompt": "What is the origin of the phrase Hello, World",
		})

		if err != nil {
			fmt.Println(w, "failed to get result from AI\n", err)
			return
		}

		fmt.Println(aiJsonResultStr)

		io.Copy(w, strings.NewReader(aiJsonResultStr))
	})

	http.HandleFunc("/ai-text-to-image", func(w http.ResponseWriter, req *http.Request) {

		// initialize AI namespace instance
		aiCaller, err := ai.New("AI")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init AI instance: %v", err)
			os.Exit(1)
		}

		aiJsonResultStr, err := aiCaller.Run("@cf/black-forest-labs/flux-1-schnell", map[string]any{
			"prompt": "a cyberpunk lizard",
		})

		if err != nil {
			fmt.Println(w, "failed to get result from AI\n", err)
			return
		}

		fmt.Println(aiJsonResultStr)

		var response struct {
			Image string `json:"image"`
		}
		err = json.Unmarshal([]byte(aiJsonResultStr), &response)

		// Decode the base64 string
		imageData, err := base64.StdEncoding.DecodeString(response.Image)
		if err != nil {
			http.Error(w, "failed to decode image", http.StatusInternalServerError)
			return
		}

		// Set the appropriate content type for the image
		w.Header().Set("Content-Type", "image/png")

		// Write the image data to the response
		w.Write(imageData)
	})

	http.HandleFunc("/ai-image-to-image", func(w http.ResponseWriter, req *http.Request) {

		// initialize AI namespace instance
		aiCaller, err := ai.New("AI")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to init AI instance: %v", err)
			os.Exit(1)
		}

		r, err := fetch.NewRequest(req.Context(), "GET", "https://pub-1fb693cb11cc46b2b2f656f51e015a2c.r2.dev/dog.png", nil)
		if err != nil {
			handleError(w, http.StatusInternalServerError, "Internal Error")
			log.Printf("failed to initialize proxy request: %v\n", err)
			return
		}

		cli := fetch.NewClient()
		resp, err := cli.Do(r, nil)
		if err != nil {
			handleError(w, http.StatusInternalServerError, "Internal Error")
			log.Printf("failed to execute proxy request: %v\n", err)
			return
		}

		defer resp.Body.Close()

		imgBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			http.Error(w, "Error reading image", http.StatusInternalServerError)
			return
		}

		aiResult, err := aiCaller.RunReader("@cf/runwayml/stable-diffusion-v1-5-img2img", map[string]any{
			"prompt": "Change to a lion",
			// "image":  imgBytes,
			"image_b64": base64.StdEncoding.EncodeToString(imgBytes),
		})

		io.Copy(w, aiResult)

		// At this point we have the this in the screen
		//  "data: {"0":137,"1":80,"2":78,"3":71,"4":13,"5":10,"6":26,"7":10,"8":0,"9":0,"10":0,"11":13,"12":73,"13":72..."

		// aiResultStr, err := io.ReadAll(aiResult)
		// if err != nil {
		// 	http.Error(w, "Error reading AI result", http.StatusInternalServerError)
		// 	return
		// }
		// reader := strings.NewReader(string(aiResultStr))

		// // Try to read the response as string...
		// buf := new(strings.Builder)
		// _, err2 := io.Copy(buf, reader)
		// if err2 != nil {
		// 	panic(err)
		// }

		// // First step: remove the prefix "data: {", and the suffix "}"
		// line := strings.TrimPrefix(buf.String(), "data: {")
		// line = strings.TrimSuffix(line, "}")

		// // Second step: split the string into parts ... "0":137,"1":80...
		// parts := strings.Split(line, ",")

		// // Third step: extract the bytes in order to create the image
		// imageBytes := make([]byte, len(parts))
		// for _, part := range parts {
		// 	kv := strings.Split(part, ":")
		// 	keyStr := strings.Trim(kv[0], `"`)
		// 	valStr := kv[1]

		// 	index, _ := strconv.Atoi(keyStr)
		// 	value, _ := strconv.Atoi(valStr)

		// 	imageBytes[index] = byte(value)
		// }

		// // You have an image... but couln't be displayed because the format is incorrect...
		// // And dont not why... the PNG format start correct but don't display the image
		// fmt.Println("Bytes:", strconv.Itoa(len(imageBytes)))

		// w.Header().Set("Content-Type", "image/png")
		// w.Write(imageBytes)

	})
	workers.Serve(nil) // use http.DefaultServeMux
}
