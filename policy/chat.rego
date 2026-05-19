package chat

default allow := true

deny contains msg if {
	some word in restricted_words
	contains(lower(input.message), word)
	msg := sprintf("Message contains restricted word: %v", [word])
}

allow := false if {
	count(deny) > 0
}

restricted_words := {
	"secret",
	"confidential",
	"password",
}
