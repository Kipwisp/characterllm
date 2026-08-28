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

	CreateCharacter = struct {
		AnalysisFailed string
		Unknown        string
		Ambiguous      string
		Injection      string
		SelectPicture  string
	}{
		AnalysisFailed: "I had trouble understanding your request: %v",
		Unknown:        "I couldn't find any reliable information on '%s'. Could you provide more details or the series they are from?",
		Ambiguous:      "I found multiple characters named '%s':\n%s\nPlease be more specific!",
		Injection:      "Nice try! I'm not falling for that prompt injection. Please provide a valid character name.",
		SelectPicture:  "Please select a profile picture:",
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
		SetMissingPrompt:     "Please provide a description. Example: /createcharacter description: Happy Barrett, 1920s",
		ImageSuccess:         "Profile picture updated successfully!",
		ImageExpired:         "Session expired or image candidates lost. Please run /setcharacter again.",
		ImageInvalid:         "Invalid image selection.",
		ImageError:           "Failed to process the selected image. Please try another one.",
		AvatarError:          "Persona set, but failed to update the server profile picture.",
		NoImageSelected:      "No image selected.",
	}

	ListCharacters = struct {
		Empty      string
		NotFound   string
		SetError   string
		SetSuccess string
		SetDetail  string
	}{
		Empty:      "No saved character cards found for this guild. Use /createcharacter to create one!",
		NotFound:   "The selected character card no longer exists or is unavailable.",
		SetError:   "An error occurred while setting the character.",
		SetSuccess: "Character set to **%s**!",
	SetDetail:  "\nCharacter: %s",
	}

	CharacterResolution = struct {
		NotFound  string
		Ambiguous string
	}{
		NotFound:  "I don't have **%s** yet. Use `/createcharacter` to research and create them.",
		Ambiguous: "Multiple saved characters match %s:\n%s\nUse `/setcharacter` with a more specific name or one of the IDs above.",
	}

	DeleteCharacter = struct {
		ConfirmPrompt string
		Deleted       string
		Cancelled     string
		Error         string
	}{
		ConfirmPrompt: "This permanently deletes **%s** and their %d chat threads. This cannot be undone.",
		Deleted:       "Deleted **%s** (%d threads).",
		Cancelled:     "Deletion cancelled.",
		Error:         "Sorry, I couldn't delete the character.",
	}

	EditCharacter = struct {
		MissingInput    string
		SectionNotFound string
		Rewriting       string
		ProposalFailed  string
		Propose         string
		Updated         string
		Rejected        string
		Expired         string
		Error           string
	}{
		MissingInput:    "Provide a `section` and an `instruction` — the new value for a name/series field, or what to change in the persona section.",
		SectionNotFound: "Section %s is not editable.",
		Rewriting:       "Rewriting **%s** — %s…",
		ProposalFailed:  "Sorry, I couldn't generate the edit proposal. Please try again.",
		Propose:         "Here is the proposed %s for **%s** — accept or reject below.",
		Updated:         "Updated **%s**: %s.",
		Rejected:        "Edit rejected — the character is unchanged.",
		Expired:         "This edit proposal is no longer available.",
		Error:           "Sorry, I couldn't update the character.",
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
		Error   string
	}{
		Success: "Chat history reset successfully.",
		Error:   "Sorry, I couldn't reset the chat history.",
	}

	General = struct {
		NoCharacterSet string
		LLMError       string
	}{
		NoCharacterSet: "No character is set for this server. Please use `/setcharacter` to choose a persona!",
		LLMError:       "Sorry, I encountered an error while generating a response.",
	}
)
