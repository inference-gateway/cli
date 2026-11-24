package rooms

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	decisiondomain "github.com/inference-gateway/cli/internal/decision/domain"
	formatting "github.com/inference-gateway/cli/internal/platform/formatting"
	roomsdomain "github.com/inference-gateway/cli/internal/rooms/domain"
)

// mentionProbability is the router's probability for a mentioned member: a
// mention addresses it outright.
const mentionProbability = 1

// maxEvidencePostWidth caps each post the decision model reads, so one pasted
// file cannot crowd out the rest of the room's evidence.
const maxEvidencePostWidth = 2000

// memberInstructions asks whether one member should act on the new post,
// given its role.
const memberInstructions = "Should the agent %q act on the new post? Its role: %s. " +
	"Answer true when the post asks for or clearly needs this role, false otherwise."

// Routing is where one post goes: the agents it mentions, the members it is
// routed to with the router's probability, and notices for its author. Tokens
// are the input tokens the decision model spent on it.
type Routing struct {
	Mentions []string
	Routes   []roomsdomain.Route
	Notices  []string
	Tokens   int
}

// Router decides which members of a room get a post. A post that pings agents
// asks no model: the pinged members get it, an @all every member, and a pinged
// outsider a notice. A post that pings nobody asks the decision model one
// predicate per member and goes to one member, the room's pickup deciding how,
// and rooms.router.fallback handles a post nobody passed.
type Router struct {
	decider decisiondomain.Decider
	config  config.RoomsConfig
}

// NewRouter returns the router for the rooms config, asking decider about posts
// that mention no member.
func NewRouter(decider decisiondomain.Decider, cfg config.RoomsConfig) *Router {
	return &Router{decider: decider, config: cfg}
}

// Route routes post in room. agents are the agents the CLI knows, with the
// descriptions the decision model reads, and history the room's earlier posts.
func (r *Router) Route(ctx context.Context, room roomsdomain.Room, agents []agentdomain.SubagentInfo, history []roomsdomain.Post, post roomsdomain.Post) (Routing, error) {
	known := agentNames(agents)
	routing := pings(room, known, post)
	if roomsdomain.MentionsEveryone(post.Text, known) {
		routing.Mentions = append(routing.Mentions, roomsdomain.EveryoneMention)
		routing.Routes = everyMember(room, mentionProbability)
	}
	if len(routing.Mentions) > 0 {
		return routing, nil
	}
	return r.routeUnmentioned(ctx, room, agents, history, post)
}

// RouteHandover routes a member's post to the members it pings, so members hand
// work to each other like teammates. It asks no model, and @all stays the
// user's: a member's @all reaches nobody.
func (r *Router) RouteHandover(room roomsdomain.Room, agents []agentdomain.SubagentInfo, post roomsdomain.Post) Routing {
	return pings(room, agentNames(agents), post)
}

// pings routes post to the members it mentions, with a notice for a pinged
// agent outside the room. A member pinging itself reaches nobody.
func pings(room roomsdomain.Room, known []string, post roomsdomain.Post) Routing {
	routing := Routing{Mentions: roomsdomain.Mentions(post.Text, known)}
	for _, agent := range routing.Mentions {
		switch {
		case agent == post.Author:
		case !slices.Contains(room.Agents, agent):
			routing.Notices = append(routing.Notices, fmt.Sprintf("@%s is not in room %s.", agent, room.Name))
		default:
			routing.Routes = append(routing.Routes, roomsdomain.Route{Member: agent, Probability: mentionProbability})
		}
	}
	return routing
}

// routeUnmentioned asks the decision model about a post that pings no agent
// and ranks the members at or above min_confidence it is offered to in turn,
// as many as the room's pickup allows. When nobody passes, fallback top offers
// it to the most relevant members just the same, so a pass still finds the
// next. Without a model only pings route, so the author gets a notice.
func (r *Router) routeUnmentioned(ctx context.Context, room roomsdomain.Room, agents []agentdomain.SubagentInfo, history []roomsdomain.Post, post roomsdomain.Post) (Routing, error) {
	var routing Routing
	if len(room.Agents) == 0 {
		routing.Notices = append(routing.Notices, fmt.Sprintf("Room %s has no members.", room.Name))
		return routing, nil
	}
	if r.config.Router.Model == "" {
		routing.Notices = append(routing.Notices, mentionNotice(room))
		return routing, nil
	}
	decisions, err := r.decider.Decide(ctx, r.config.Router.Model, r.evidence(history, post), memberQuestions(room, agents))
	if err != nil {
		return routing, fmt.Errorf("routing a post in room %s: %w", room.Name, err)
	}
	routing.Tokens = decisions.InputTokens
	ranked := rankedMembers(room, decisions)
	for _, route := range ranked[:min(len(ranked), r.offers(room))] {
		if route.Probability >= r.config.Router.MinConfidence {
			routing.Routes = append(routing.Routes, route)
		}
	}
	if len(routing.Routes) > 0 {
		return routing, nil
	}
	if cmp.Or(r.config.Router.Fallback, config.RoomsFallbackTop) == config.RoomsFallbackTop {
		routing.Routes = ranked[:min(len(ranked), r.offers(room))]
		return routing, nil
	}
	routing.Notices = append(routing.Notices, mentionNotice(room))
	return routing, nil
}

// evidence is what the decision model reads: the last context_posts posts of
// the room and the new one, each under its author.
func (r *Router) evidence(history []roomsdomain.Post, post roomsdomain.Post) string {
	recent := history[max(0, len(history)-r.config.ContextPosts):]
	var b strings.Builder
	b.WriteString("Recent posts in the room:\n")
	for _, earlier := range recent {
		writeEvidencePost(&b, earlier)
	}
	b.WriteString("\nThe new post:\n")
	writeEvidencePost(&b, post)
	return b.String()
}

func writeEvidencePost(b *strings.Builder, post roomsdomain.Post) {
	fmt.Fprintf(b, "%s: %s\n", post.Author, formatting.TruncateText(post.Text, maxEvidencePostWidth))
}

// memberQuestions asks one predicate per member, named by the agent so the
// answer maps back without parsing.
func memberQuestions(room roomsdomain.Room, agents []agentdomain.SubagentInfo) []decisiondomain.Question {
	descriptions := make(map[string]string, len(agents))
	for _, agent := range agents {
		descriptions[agent.Name] = agent.Description
	}
	questions := make([]decisiondomain.Question, 0, len(room.Agents))
	for _, member := range room.Agents {
		questions = append(questions, decisiondomain.Question{
			Name:         member,
			Type:         decisiondomain.Predicate,
			Instructions: fmt.Sprintf(memberInstructions, member, cmp.Or(descriptions[member], "no description")),
		})
	}
	return questions
}

// offers is how many members a post that pings nobody is offered to in turn:
// max_members in a room that offers, its top match alone in one that does not.
func (r *Router) offers(room roomsdomain.Room) int {
	if room.Pickup == roomsdomain.PickupTop {
		return 1
	}
	return r.config.Router.EffectiveMaxMembers()
}

// everyMember routes a post to every member of room with probability.
func everyMember(room roomsdomain.Room, probability float64) []roomsdomain.Route {
	routes := make([]roomsdomain.Route, 0, len(room.Agents))
	for _, member := range room.Agents {
		routes = append(routes, roomsdomain.Route{Member: member, Probability: probability})
	}
	return routes
}

// rankedMembers scores every member, the most relevant first and room order on
// a tie. A member the model did not answer for scores zero.
func rankedMembers(room roomsdomain.Room, decisions decisiondomain.Decisions) []roomsdomain.Route {
	routes := make([]roomsdomain.Route, 0, len(room.Agents))
	for _, member := range room.Agents {
		answer, _ := decisions.ByName(member)
		routes = append(routes, roomsdomain.Route{Member: member, Probability: answer.Probability})
	}
	slices.SortStableFunc(routes, func(a, b roomsdomain.Route) int {
		return cmp.Compare(b.Probability, a.Probability)
	})
	return routes
}

func mentionNotice(room roomsdomain.Room) string {
	mentions := make([]string, 0, len(room.Agents))
	for _, member := range room.Agents {
		mentions = append(mentions, "@"+member)
	}
	return fmt.Sprintf("No member of room %s was addressed. Mention one: %s", room.Name, strings.Join(mentions, ", "))
}

func agentNames(agents []agentdomain.SubagentInfo) []string {
	names := make([]string, 0, len(agents))
	for _, agent := range agents {
		names = append(names, agent.Name)
	}
	return names
}
