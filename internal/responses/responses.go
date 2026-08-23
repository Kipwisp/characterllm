package responses

// Response messages used across the bot.
const (
	MsgChatResetSuccess         = "Chat history reset successfully."
	MsgStatusOnline             = "✅ LLM Server is online! Latency: %v"
	MsgStatusOffline            = "❌ LLM Server is unreachable: %v"
	MsgCharCreating             = "Creating persona for \"%s\"..."
	MsgCharNotFound             = "Failed to find character '%s': %v"
	MsgCharImageSuccess         = "Profile picture updated successfully!"
	MsgCharImageExpired         = "Session expired or image candidates lost. Please run /setcharacter again."
	MsgCharImageInvalid         = "Invalid image selection."
	MsgCharImageError           = "Failed to process the selected image. Please try another one."
	MsgCharSetFinalizationError = "An unexpected error occurred while finalizing your character. Please run `/setcharacter` again."
	MsgCharAvatarError          = "Persona set, but failed to update the server profile picture."
	MsgNoCharacterSet           = "No character is set for this server. Please use `/setcharacter` to choose a persona!"
	MsgLLMError                 = "Sorry, I encountered an error while generating a response."
	MsgSetCharMissingPrompt     = "Please provide a prompt. Example: /setcharacter name: Happy Barret"
	MsgNoImageSelected          = "No image selected."
)
