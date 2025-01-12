package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/spf13/viper"
)

type Config struct {
	Discord    Discord    `mapstructure:"discord"`
	Openrouter Openrouter `mapstructure:"openrouter"`
	EmbedColor int        `mapstructure:"embed_color"`
}

type Discord struct {
	Token   string `mapstructure:"token"`
	AppID   string `mapstructure:"app_id"`
	GuildID string `mapstructure:"guild_id"`
}

type Openrouter struct {
	ApiKey               string  `mapstructure:"api_key"`
	Model                string  `mapstructure:"model"`
	ModelPromptPrice     float32 `mapstructure:"model_prompt_price"`
	ModelCompletionPrice float32 `mapstructure:"model_completion_price"`
	SystemMessage        string  `mapstructure:"system_message"`
	BaseUrl              string  `mapstructure:"base_url"`
}

type ReqMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RequestBody struct {
	Model    string       `json:"model"`
	Messages []ReqMessage `json:"messages"`
}

type Response struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// var session *discordgo.Session
var config Config

var (
	commands = []*discordgo.ApplicationCommand{
		{
			Name:        "chat",
			Description: "Chat with AI",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "request",
					Description: "Request Question",
					Required:    true,
				},
			},
		},
	}
	commandHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"chat": func(s *discordgo.Session, i *discordgo.InteractionCreate) {
			options := i.ApplicationCommandData().Options
			optionMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(options))
			for _, opt := range options {
				optionMap[opt.Name] = opt
			}

			s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Content: "Thinking ...",
				},
			})

			response_arr := SplitLonger("**Request :** ***"+optionMap["request"].StringValue()+"***\n\n"+askAi(optionMap["request"].StringValue()), 4000)
			fmt.Println(response_arr)
			for _, message := range response_arr {

				_, err := s.ChannelMessageSendEmbed(i.ChannelID, &discordgo.MessageEmbed{
					Color:       config.EmbedColor,
					Description: message,
				})

				if err != nil {
					log.Println(err)
					s.FollowupMessageCreate(i.Interaction, true, &discordgo.WebhookParams{
						Content: "Something went wrong",
					})
					return
				}
			}
		},
	}
)

func main() {
	// Initialize Viper
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")

	// Read the configuration file
	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("Error reading config file, %s", err)
	}

	// Unmarshal the configuration into a struct
	err = viper.Unmarshal(&config)
	if err != nil {
		log.Fatalf("Unable to unmarshal config , %s", err)
	}

	// Make new discord function
	session, _ := discordgo.New("Bot " + config.Discord.Token)

	session.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if h, ok := commandHandlers[i.ApplicationCommandData().Name]; ok {
			h(s, i)
		}
	})
	session.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Logged in as: %v#%v", s.State.User.Username, s.State.User.Discriminator)
	})
	err = session.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}

	log.Println("Adding commands...")
	registeredCommands := make([]*discordgo.ApplicationCommand, len(commands))
	for i, v := range commands {
		cmd, err := session.ApplicationCommandCreate(session.State.User.ID, config.Discord.GuildID, v)
		if err != nil {
			log.Panicf("Cannot create '%v' command: %v", v.Name, err)
		}
		registeredCommands[i] = cmd
	}

	defer session.Close()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Println("Press Ctrl+C to exit")
	<-stop

	log.Println("Removing commands...")
	registeredCommands, err = session.ApplicationCommands(session.State.User.ID, config.Discord.GuildID)
	if err != nil {
		log.Fatalf("Could not fetch registered commands: %v", err)
	}

	for _, v := range registeredCommands {
		err := session.ApplicationCommandDelete(session.State.User.ID, config.Discord.GuildID, v.ID)
		if err != nil {
			log.Panicf("Cannot delete '%v' command: %v", v.Name, err)
		}
	}

	log.Println("Gracefully shutting down.")

}

func askAi(request string) string {
	reqBody := RequestBody{
		Model: config.Openrouter.Model,
		Messages: []ReqMessage{
			{
				Role:    "system",
				Content: config.Openrouter.SystemMessage,
			},
			{
				Role:    "user",
				Content: request,
			},
		},
	}

	jsonBody, err := json.Marshal(reqBody)

	if err != nil {
		log.Println(err)
		return ""
	}
	req, err := http.NewRequest("POST", config.Openrouter.BaseUrl, bytes.NewBuffer(jsonBody))
	if err != nil {
		log.Println(err)
		return ""
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", config.Openrouter.ApiKey))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println(err)
		return time.Now().String() + " - API call error : 001"
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println(err)
		return time.Now().String() + " - API call error : 002"
	}

	var parsed Response
	err = json.Unmarshal(body, &parsed)
	if err != nil {
		log.Println(err)
		return time.Now().String() + " - API call error : 003"
	}

	return fmt.Sprintf("%s\n\nPrompt Token: %d\nCompletion Token: %d\nTotal Token: %d Estimated Total Cost: ($%f)",
		parsed.Choices[0].Message.Content,
		parsed.Usage.PromptTokens,
		parsed.Usage.CompletionTokens,
		parsed.Usage.TotalTokens,
		(config.Openrouter.ModelPromptPrice/1000000)*float32(parsed.Usage.PromptTokens)+(config.Openrouter.ModelCompletionPrice/1000000)*float32(parsed.Usage.CompletionTokens))
}

func SplitLonger(longer string, limit int) []string {
	var result []string
	words := strings.Split(longer, " ")
	var currentString string
	for _, word := range words {
		if len(currentString)+len(word)+1 > limit {
			result = append(result, currentString)
			currentString = word
		} else {
			if currentString != "" {
				currentString += " "
			}
			currentString += word
		}
	}
	result = append(result, currentString)
	return result
}
