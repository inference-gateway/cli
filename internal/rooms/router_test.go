package rooms

import (
	"errors"
	"testing"

	require "github.com/stretchr/testify/require"

	decisionmocks "github.com/inference-gateway/cli/tests/mocks/decision"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

const testRouterModel = "anthropic/claude-haiku-5-5"

var (
	testRoom   = roomsdomain.Room{Name: "release", Agents: []string{"tester", "writer"}}
	testAgents = []agentdomain.SubagentInfo{
		{Name: "tester", Role: "qa engineer", Description: "Runs and fixes the test suite"},
		{Name: "writer", Description: "Writes release notes"},
		{Name: "reviewer", Description: "Reviews pull requests"},
	}
)

func probabilities(byMember map[string]float64) decisiondomain.Decisions {
	var decisions decisiondomain.Decisions
	for member, probability := range byMember {
		decisions.Answers = append(decisions.Answers, decisiondomain.Answer{
			Name: member, Type: decisiondomain.Predicate, Probability: probability,
		})
	}
	return decisions
}

func TestRouterRoute(t *testing.T) {
	tests := []struct {
		name        string
		model       string
		fallback    string
		text        string
		decisions   decisiondomain.Decisions
		decideErr   error
		wantRoutes  []roomsdomain.Route
		wantNotice  string
		wantDecides int
		wantErr     string
	}{
		{
			name:       "a mention of a member routes to it alone without asking the model",
			model:      testRouterModel,
			text:       "@tester run the suite",
			wantRoutes: []roomsdomain.Route{{Member: "tester", Probability: 1}},
		},
		{
			name:       "mentions of two members route to both",
			text:       "@writer and @tester, sync up",
			wantRoutes: []roomsdomain.Route{{Member: "writer", Probability: 1}, {Member: "tester", Probability: 1}},
		},
		{
			name:       "@all routes to every member without asking the model",
			model:      testRouterModel,
			text:       "hi @all!",
			wantRoutes: []roomsdomain.Route{{Member: "tester", Probability: 1}, {Member: "writer", Probability: 1}},
		},
		{
			name:        "a greeting without @all is offered like any post",
			model:       testRouterModel,
			text:        "Hi all",
			decisions:   probabilities(map[string]float64{"tester": 0.3, "writer": 0.6}),
			wantRoutes:  []roomsdomain.Route{{Member: "writer", Probability: 0.6}},
			wantDecides: 1,
		},
		{
			name:       "a ping of an agent outside the room gets a notice and asks no model",
			model:      testRouterModel,
			text:       "@reviewer look at this",
			wantNotice: "@reviewer is not in room release",
		},
		{
			name:       "a ping of a member and an outsider routes to the member and notes the outsider",
			model:      testRouterModel,
			text:       "@tester and @reviewer look at this",
			wantRoutes: []roomsdomain.Route{{Member: "tester", Probability: 1}},
			wantNotice: "@reviewer is not in room release",
		},
		{
			name:        "a file reference is left alone and the model routes the post",
			model:       testRouterModel,
			text:        "summarise @README.md",
			decisions:   probabilities(map[string]float64{"tester": 0.1, "writer": 0.9}),
			wantRoutes:  []roomsdomain.Route{{Member: "writer", Probability: 0.9}},
			wantDecides: 1,
		},
		{
			name:        "a dot-slash path names the file of a member's name, not the member",
			model:       testRouterModel,
			text:        "what does @./tester do",
			decisions:   probabilities(map[string]float64{"tester": 0.3, "writer": 0.6}),
			wantRoutes:  []roomsdomain.Route{{Member: "writer", Probability: 0.6}},
			wantDecides: 1,
		},
		{
			name:        "every member at or above min_confidence gets the post, the most relevant first",
			model:       testRouterModel,
			text:        "cut the release",
			decisions:   probabilities(map[string]float64{"tester": 0.5, "writer": 0.7}),
			wantRoutes:  []roomsdomain.Route{{Member: "writer", Probability: 0.7}, {Member: "tester", Probability: 0.5}},
			wantDecides: 1,
		},
		{
			name:        "fallback top offers a post nobody passed to the most relevant members in turn",
			model:       testRouterModel,
			fallback:    config.RoomsFallbackTop,
			text:        "hello",
			decisions:   probabilities(map[string]float64{"tester": 0.3, "writer": 0.4}),
			wantRoutes:  []roomsdomain.Route{{Member: "writer", Probability: 0.4}, {Member: "tester", Probability: 0.3}},
			wantDecides: 1,
		},
		{
			name:        "fallback none routes a post nobody passed to nobody and asks for a mention",
			model:       testRouterModel,
			fallback:    config.RoomsFallbackNone,
			text:        "hello",
			decisions:   probabilities(map[string]float64{"tester": 0.3, "writer": 0.4}),
			wantNotice:  "Mention one: @tester, @writer",
			wantDecides: 1,
		},
		{
			name:       "without a model an unmentioned post routes nowhere and asks for a mention",
			text:       "hello",
			wantNotice: "Mention one: @tester, @writer",
		},
		{
			name:        "a failed decision request fails the routing",
			model:       testRouterModel,
			text:        "hello",
			decideErr:   errors.New("the Decisions API is not enabled"),
			wantDecides: 1,
			wantErr:     "Decisions API is not enabled",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decider := &decisionmocks.FakeDecider{}
			decider.DecideReturns(tt.decisions, tt.decideErr)
			cfg := *config.DefaultRoomsConfig()
			cfg.Router.Model = tt.model
			if tt.fallback != "" {
				cfg.Router.Fallback = tt.fallback
			}

			routing, err := NewRouter(decider, cfg).Route(t.Context(), testRoom, testAgents, nil, roomsdomain.Post{Author: "user", Text: tt.text})
			require.Equal(t, tt.wantDecides, decider.DecideCallCount(), "decision requests")
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantRoutes, routing.Routes)
			if tt.wantNotice == "" {
				require.Empty(t, routing.Notices)
				return
			}
			require.Len(t, routing.Notices, 1)
			require.Contains(t, routing.Notices[0], tt.wantNotice)
		})
	}
}

// TestRouterAsksOnePredicatePerMember pins the decision request: one call on
// the router model, one predicate per member named by the agent and carrying
// its description, and only the last context_posts posts as evidence.
func TestRouterAsksOnePredicatePerMember(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decider.DecideReturns(probabilities(map[string]float64{"tester": 0.9}), nil)
	cfg := *config.DefaultRoomsConfig()
	cfg.Router.Model = testRouterModel
	cfg.ContextPosts = 1
	history := []roomsdomain.Post{
		{Author: "user", Text: "an old post the router no longer shows"},
		{Author: "writer", Text: "the notes are drafted"},
	}

	_, err := NewRouter(decider, cfg).Route(t.Context(), testRoom, testAgents, history, roomsdomain.Post{Author: "user", Text: "now run the tests"})
	require.NoError(t, err)

	_, model, evidence, questions := decider.DecideArgsForCall(0)
	require.Equal(t, testRouterModel, model)
	require.Len(t, questions, len(testRoom.Agents), "one predicate per member")
	for i, member := range testRoom.Agents {
		require.Equal(t, member, questions[i].Name)
		require.Equal(t, decisiondomain.Predicate, questions[i].Type)
	}
	require.Contains(t, questions[0].Instructions, "Its role: qa engineer. What it does: Runs and fixes the test suite")
	require.Contains(t, questions[1].Instructions, "Its role: not named. What it does: Writes release notes")
	require.Contains(t, evidence, "writer: the notes are drafted")
	require.Contains(t, evidence, "user: now run the tests")
	require.NotContains(t, evidence, "an old post", "only the last context_posts posts are evidence")
}

// TestRouterCapsUnmentionedPosts pins max_members: in a noisy room an
// unmentioned post reaches only the most relevant members that passed, while
// members the author mentions are never capped.
func TestRouterCapsUnmentionedPosts(t *testing.T) {
	members := []string{"a", "b", "c", "d", "e"}
	room := roomsdomain.Room{Name: "busy", Agents: members}
	agents := make([]agentdomain.SubagentInfo, 0, len(members))
	for _, member := range members {
		agents = append(agents, agentdomain.SubagentInfo{Name: member})
	}
	everyonePasses := probabilities(map[string]float64{"a": 0.6, "b": 0.9, "c": 0.7, "d": 0.55, "e": 0.8})

	tests := []struct {
		name        string
		maxMembers  int
		text        string
		decisions   decisiondomain.Decisions
		wantMembers []string
	}{
		{"the default sends it to the two most relevant", 0, "ship it", everyonePasses, []string{"b", "e"}},
		{"a higher cap sends it to more", 3, "ship it", everyonePasses, []string{"b", "e", "c"}},
		{"mentions are never capped", 1, "@a @c @d ship it", everyonePasses, []string{"a", "c", "d"}},
		{"@all is never capped", 1, "@all ship it", everyonePasses, members},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decider := &decisionmocks.FakeDecider{}
			decider.DecideReturns(tt.decisions, nil)
			cfg := *config.DefaultRoomsConfig()
			cfg.Router.Model = testRouterModel
			cfg.Router.MaxMembers = tt.maxMembers

			routing, err := NewRouter(decider, cfg).Route(t.Context(), room, agents, nil, roomsdomain.Post{Author: "user", Text: tt.text})
			require.NoError(t, err)
			got := make([]string, 0, len(routing.Routes))
			for _, route := range routing.Routes {
				got = append(got, route.Member)
			}
			require.Equal(t, tt.wantMembers, got)
		})
	}
}

func TestRouterReportsTheTokensItsDecisionSpent(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decisions := probabilities(map[string]float64{"tester": 0.9})
	decisions.InputTokens = 412
	decider.DecideReturns(decisions, nil)
	cfg := *config.DefaultRoomsConfig()
	cfg.Router.Model = testRouterModel

	routing, err := NewRouter(decider, cfg).Route(t.Context(), testRoom, testAgents, nil, roomsdomain.Post{Author: "user", Text: "run the suite"})
	require.NoError(t, err)
	require.Equal(t, 412, routing.Tokens)
}

// TestRouterOffersAPostToOneMemberAtATime pins the pickup of a post that pings
// nobody: a room that offers ranks up to max_members members to offer it to in
// turn, the best first, and a room that hands posts to its top match routes it
// to that one member only.
func TestRouterOffersAPostToOneMemberAtATime(t *testing.T) {
	decider := &decisionmocks.FakeDecider{}
	decider.DecideReturns(probabilities(map[string]float64{"tester": 0.7, "writer": 0.9}), nil)
	cfg := *config.DefaultRoomsConfig()
	cfg.Router.Model = testRouterModel
	router := NewRouter(decider, cfg)
	post := roomsdomain.Post{Author: "user", Text: "what's the weather?"}

	offering := testRoom
	offering.Pickup = roomsdomain.PickupOffer
	routing, err := router.Route(t.Context(), offering, testAgents, nil, post)
	require.NoError(t, err)
	require.Equal(t, []roomsdomain.Route{{Member: "writer", Probability: 0.9}, {Member: "tester", Probability: 0.7}}, routing.Routes)

	topMatch := testRoom
	topMatch.Pickup = roomsdomain.PickupTop
	routing, err = router.Route(t.Context(), topMatch, testAgents, nil, post)
	require.NoError(t, err)
	require.Equal(t, []roomsdomain.Route{{Member: "writer", Probability: 0.9}}, routing.Routes)

	decider.DecideReturns(probabilities(map[string]float64{"tester": 0.1, "writer": 0.24}), nil)
	routing, err = router.Route(t.Context(), topMatch, testAgents, nil, post)
	require.NoError(t, err)
	require.Equal(t, []roomsdomain.Route{{Member: "writer", Probability: 0.24}}, routing.Routes, "the fallback hands a top-match room's post to one member too")
}

// TestRouterHandsAMembersPostToThePingedMembers pins a member's hand-over: its
// post reaches the members it pings as a ping, and nobody else - not itself,
// not an outsider, whom the room notes, and not the whole room through @all,
// which stays the user's.
func TestRouterHandsAMembersPostToThePingedMembers(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		wantRoutes []roomsdomain.Route
		wantNotice string
	}{
		{
			name:       "a pinged member gets it",
			text:       "@tester, you're up: the user wants a joke from someone other than me",
			wantRoutes: []roomsdomain.Route{{Member: "tester", Probability: 1}},
		},
		{
			name: "a member pinging itself reaches nobody",
			text: "@writer here, done",
		},
		{
			name:       "a pinged outsider reaches nobody and is noted",
			text:       "@reviewer take a look",
			wantNotice: "@reviewer is not in room release.",
		},
		{
			name: "@all stays the user's",
			text: "@all thanks everyone",
		},
		{
			name: "a file reference is no ping",
			text: "see @README.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decider := &decisionmocks.FakeDecider{}
			routing := NewRouter(decider, *config.DefaultRoomsConfig()).RouteHandover(testRoom, testAgents, roomsdomain.Post{Author: "writer", Text: tt.text})
			require.Equal(t, tt.wantRoutes, routing.Routes)
			require.Zero(t, decider.DecideCallCount(), "a member's post asks no model")
			if tt.wantNotice == "" {
				require.Empty(t, routing.Notices)
				return
			}
			require.Equal(t, []string{tt.wantNotice}, routing.Notices)
		})
	}
}
