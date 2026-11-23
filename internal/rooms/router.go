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
// routed to with the router's probability, and notices for its author.
type Routing struct {
	Mentions []string
	Routes   []roomsdomain.Route
	Notices  []string
}

// Router decides which members of a room get a post. A post that pings agents
// asks no model: the pinged members get it and a pinged outsider a notice. A
// post that pings nobody asks the decision model one predicate per member, and
// rooms.router.fallback handles a post nobody passed.
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
	routing := Routing{Mentions: roomsdomain.Mentions(post.Text, agentNames(agents))}
	for _, agent := range routing.Mentions {
		if !slices.Contains(room.Agents, agent) {
			routing.Notices = append(routing.Notices, fmt.Sprintf("@%s is not in room %s.", agent, room.Name))
			continue
		}
		routing.Routes = append(routing.Routes, roomsdomain.Route{Member: agent, Probability: mentionProbability})
	}
	if len(routing.Mentions) > 0 {
		return routing, nil
	}
	return r.routeUnmentioned(ctx, room, agents, history, post)
}

// routeUnmentioned asks the decision model about a post that pings no agent
// and routes it to the max_members most relevant members at or above
// min_confidence, else by the fallback. Without a model only pings route, so
// the author gets a notice.
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
	ranked := rankedMembers(room, decisions)
	maxMembers := cmp.Or(r.config.Router.MaxMembers, config.DefaultRoomsMaxMembers)
	for _, route := range ranked[:min(len(ranked), maxMembers)] {
		if route.Probability >= r.config.Router.MinConfidence {
			routing.Routes = append(routing.Routes, route)
		}
	}
	if len(routing.Routes) > 0 {
		return routing, nil
	}
	if cmp.Or(r.config.Router.Fallback, config.RoomsFallbackTop) == config.RoomsFallbackTop {
		routing.Routes = append(routing.Routes, ranked[0])
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
