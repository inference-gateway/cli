package guardrails

# Block A2A requests that carry a payment card number.
# This is a simple example. In production, use the built-in detectors,
# which include Luhn validation.
main := {"action": "block", "message": "request contains sensitive payment information"} if {
	input.path == "/a2a"
	contains(input.request.body, "4111")
}
