package responses

// Grouped response messages associated with specific commands and general bot behavior.
var (
	Status = struct {
		Online  string
		Offline string
	}{
		Online:  "✅ LLM Server is online! Latency: %v",
		Offline: "❌ LLM Server is unreachable: %v",
	}

	SetCharacter = struct {
		Creating             string
		NotFound             string
		SetFinalizationError string
		SetMissingPrompt     string
		ImageSuccess         string
		ImageExpired         string
		ImageInvalid         string
		ImageError           string
		AvatarError          string
		NoImageSelected      string
	}{
		Creating:             "Creating persona for \"%s\"...",
		NotFound:             "Failed to find character '%s': %v",
		SetFinalizationError: "An unexpected error occurred while finalizing your character. Please run `/setcharacter` again.",
		SetMissingPrompt:     "Please provide a prompt. Example: /setcharacter name: Happy Barret",
		ImageSuccess:         "Profile picture updated successfully!",
		ImageExpired:         "Session expired or image candidates lost. Please run /setcharacter again.",
		ImageInvalid:         "Invalid image selection.",
		ImageError:           "Failed to process the selected image. Please try another one.",
		AvatarError:          "Persona set, but failed to update the server profile picture.",
		NoImageSelected:      "No image selected.",
	}

	ListCharacters = struct {
		Empty        string
		SelectPrompt string
		NoSelected   string
		NotFound     string
		SetError     string
		SetSuccess   string
	}{
		Empty:        "No saved character cards found for this guild. Use /setcharacter to create one!",
		SelectPrompt: "Select a character to set as active:",
		NoSelected:   "No character selected.",
		NotFound:     "The selected character card no longer exists or is unavailable.",
		SetError:     "An error occurred while setting the character.",
		SetSuccess:   "Character set to **%s**!",
	}

	SetAvatar = struct {
		Success       string
		NoCharacter   string
		MissingSource string
		DownloadError string
		TooLarge      string
		AvatarError   string
	}{
		Success:       "Avatar updated successfully!",
		NoCharacter:   "No active character in this server. Use /setcharacter first.",
		MissingSource: "Provide an image via the image option.",
		DownloadError: "Failed to download the image.",
		TooLarge:      "That image is too large to use as a Discord avatar.",
		AvatarError:   "The image was saved, but Discord rejected the avatar update.",
	}

	ResetChat = struct {
		Success string
	}{
		Success: "Chat history reset successfully.",
	}

	General = struct {
		NoCharacterSet string
		LLMError       string
	}{
		NoCharacterSet: "No character is set for this server. Please use `/setcharacter` to choose a persona!",
		LLMError:       "Sorry, I encountered an error while generating a response.",
	}
)
