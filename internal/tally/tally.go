// Package tally counts a season's ballots by instant-runoff voting.
//
// It is deliberately pure: no HTTP, no templates, no database types in any
// signature. Ballots go in, a Result with every round of the count in it comes
// out. Nothing in here reads a clock, a map's iteration order, or a random
// number, so the same ballots always produce the same Result — byte for byte,
// on any machine, in any order the caller happened to load the rows.
//
// That shape is the point. Thirty people are going to argue about the outcome,
// and "the computer says so" does not survive that conversation. What survives
// it is a printable round-by-round breakdown that anybody can check by hand,
// and a count that gives the same answer when it is re-run.
//
// # THE RULES THIS PACKAGE DECIDES
//
// Instant-runoff has a specification-shaped hole in the middle of it. Three
// questions have no single right answer and every implementation picks one;
// picking silently is how two people end up hand-counting the same ballots and
// getting different winners. These are the picks, and Result carries enough
// information to show each of them happening.
//
//  1. EXHAUSTED BALLOTS AND THE MAJORITY THRESHOLD. A voter ranks three films
//     out of twelve. Once all three are eliminated their ballot is exhausted
//     and counts for nobody. The threshold to win is recomputed each round as
//     a majority of the ballots STILL COUNTING — Round.Continuing — and not
//     held at a majority of the ballots originally cast.
//
//     The alternative, a fixed threshold, is defensible in an election where
//     everyone ranks everyone. It is not defensible here. With three ranks and
//     a dozen films, exhaustion will be heavy by the middle rounds, a fixed
//     threshold would usually be unreachable by anybody, and the count would
//     degenerate every year into "keep eliminating until one is left" — which
//     is not instant-runoff, it is a plurality count wearing its coat. A
//     recomputed threshold keeps the sentence the site prints ("it won with 11
//     of 19") true of the ballots that were actually in play.
//
//  2. TIES FOR ELIMINATION. When several candidates are level at the bottom,
//     the tie is broken backwards: compare their counts in the previous round,
//     then the one before that, back to the first. Whoever was behind earliest
//     goes out. When they were level in every single round there is nothing
//     left to compare, and the fallback is the lowest candidate ID. That is
//     arbitrary, and it is arbitrary on purpose: the property worth protecting
//     is that a re-run gives the same answer, and every non-arbitrary tie-break
//     left at that point (a coin, a timestamp, the order rows came back from
//     Postgres) gives up reproducibility to buy fairness nobody can verify.
//     Round.TieBreak records that it happened and which rule settled it.
//
//  3. THE ARBITRARY FALLBACK NEVER PICKS THE WINNER. It can decide which of
//     two equally-placed losers is eliminated first. It is never allowed to
//     decide who wins, because a winner chosen by ID order is not a winner,
//     it is a coin flip with a straight face. So when every remaining
//     candidate is level — each of them simultaneously the leader and the
//     last — the count stops and reports a dead heat. Result.Tied names them.
//     A dead heat is a real outcome here, not an error: result.winner_movie_id
//     is nullable in the schema for exactly this, and the theme packs have
//     outcome.tied.* written for it. Thirty voters is few enough that this
//     will happen, not a hypothetical.
//
// # HIDDEN FILMS
//
// movie.hidden is a soft delete, and the ballot rows pointing at a hidden film
// survive it. So the exclusion has to happen here rather than being assumed of
// the caller (nap-8yw): Count takes every candidate WITH its Hidden flag and
// drops the hidden ones itself, and a preference for a hidden film is skipped
// so the ballot falls through to its next surviving choice rather than being
// thrown away. A hidden film cannot appear in a round, cannot be transferred
// to, and cannot win. Handing this package a pre-filtered list would work too,
// and would mean the guarantee lived in whichever caller remembered it.
//
// # RANKS ARE AN ORDER, NOT AN INDEX
//
// ballot_entry.rank has no upper bound and no requirement to be dense — that
// is a deliberate schema decision (nap-m1a), which makes handling it a
// requirement on this package. A ballot of ranks (1, 2, 4) must behave exactly
// like (1, 2, 3), and (10, 20, 30) like both. Count sorts the preferences and
// uses their ORDER; the numbers themselves never reach the arithmetic.
package tally

import (
	"fmt"
	"slices"
	"strings"
)

// Candidate is one film that could win.
//
// Hidden comes in rather than being filtered out by the caller so that the
// exclusion is this package's guarantee and is testable here. See the package
// comment.
type Candidate struct {
	// ID identifies the film. Opaque: it is compared and sorted, never parsed.
	// In this application it is movie.id, but nothing here knows that.
	ID string

	// Hidden is movie.hidden — a withdrawn or moderated submission. A hidden
	// candidate takes no part in the count and cannot win.
	Hidden bool
}

// Preference is one row of somebody's ballot: a film, at a position.
//
// Rank is the raw ballot_entry.rank and may be sparse, may start anywhere and
// has no upper bound. Only its order relative to the other preferences on the
// same ballot is used.
type Preference struct {
	Movie string
	Rank  int
}

// Ballot is one voter's ranked preferences.
//
// Preferences may arrive in any order; Count sorts them. Voter is carried for
// traceability only — it is reported back in Result.Ignored and takes no part
// in any decision, so that no property of the count can depend on who cast a
// ballot or on the order the ballots were loaded.
type Ballot struct {
	Voter       string
	Preferences []Preference
}

// Outcome is how a round ended.
type Outcome int

const (
	// OutcomeEliminated: nobody reached the threshold, so the last-placed
	// candidate went out and their ballots moved on.
	OutcomeEliminated Outcome = iota

	// OutcomeElected: somebody reached the threshold, or was the only
	// candidate left standing.
	OutcomeElected

	// OutcomeDeadHeat: every remaining candidate was level, so there was no
	// non-arbitrary candidate to eliminate and no majority to elect.
	OutcomeDeadHeat
)

func (o Outcome) String() string {
	switch o {
	case OutcomeEliminated:
		return "eliminated"
	case OutcomeElected:
		return "elected"
	case OutcomeDeadHeat:
		return "dead heat"
	default:
		return fmt.Sprintf("Outcome(%d)", int(o))
	}
}

// TieBreakRule is how a tie at the bottom of a round was settled.
type TieBreakRule int

const (
	// TieBreakNone: there was no tie, one candidate was alone at the bottom.
	TieBreakNone TieBreakRule = iota

	// TieBreakEarlierRound: the tied candidates were separated by their counts
	// in an earlier round. TieBreak.Round says which one.
	TieBreakEarlierRound

	// TieBreakLowestID: they were level in every round including the first, so
	// the lowest candidate ID was eliminated. Arbitrary, deterministic, and
	// never used to choose a winner. See the package comment.
	TieBreakLowestID
)

func (r TieBreakRule) String() string {
	switch r {
	case TieBreakNone:
		return "none"
	case TieBreakEarlierRound:
		return "earlier round"
	case TieBreakLowestID:
		return "lowest id"
	default:
		return fmt.Sprintf("TieBreakRule(%d)", int(r))
	}
}

// TieBreak records a tie at the bottom of a round and how it was resolved.
//
// It is reported rather than kept internal because a tie-break is the part of
// a count somebody is most likely to want to check, and TieBreakLowestID in
// particular is a result the group is entitled to see labelled as arbitrary
// instead of finding out later.
type TieBreak struct {
	// Tied are the candidates that were level at the bottom, sorted by ID.
	// Empty when Rule is TieBreakNone.
	Tied []string

	Rule TieBreakRule

	// Round is the earlier round whose counts separated them, when Rule is
	// TieBreakEarlierRound. Zero otherwise.
	Round int
}

// Tally is one candidate's votes in one round.
type Tally struct {
	Movie string
	Votes int
}

// Transfer is where an eliminated candidate's ballots went.
//
// To is empty for the ballots that had no surviving preference left and became
// exhausted. The transfers of a round always sum to the eliminated candidate's
// count in that round, which is the arithmetic a person checking the count by
// hand will do first.
type Transfer struct {
	To    string
	Votes int
}

// Round is one pass of the count, with enough in it to redo that pass by hand.
type Round struct {
	// Number is 1-based.
	Number int

	// Counts is every candidate still in the count at the start of this round,
	// most votes first, ties broken by ID so that the report is stable.
	Counts []Tally

	// Continuing is the number of ballots counting for somebody this round;
	// Exhausted is the number that have run out of surviving preferences. They
	// sum to Result.Ballots in every round.
	Continuing int
	Exhausted  int

	// Threshold is the votes needed to win outright this round: a strict
	// majority of Continuing. Recomputed each round — see the package comment.
	Threshold int

	Outcome Outcome

	// Elected is set when Outcome is OutcomeElected.
	Elected string

	// Eliminated and Transfers are set when Outcome is OutcomeEliminated.
	Eliminated string
	Transfers  []Transfer

	// TieBreak describes the tie at the bottom of this round, if there was one.
	TieBreak TieBreak

	// DeadHeat is the candidates left level when Outcome is OutcomeDeadHeat.
	DeadHeat []string
}

// Result is the whole count.
//
// Exactly one of Winner and Tied is set. Winner == "" and len(Tied) == 0
// together mean there was nothing to count: no candidates at all.
type Result struct {
	// Winner is the winning candidate, or empty when the count did not produce
	// one.
	Winner string

	// Tied are the candidates left level in a dead heat, sorted by ID. Empty
	// when there is a Winner.
	Tied []string

	// Rounds is the count, in order. Every round after the first follows an
	// elimination in the one before it.
	Rounds []Round

	// Candidates are the candidates that took part, sorted by ID.
	Candidates []string

	// Excluded are the candidates left out because they were hidden, sorted by
	// ID. They appear nowhere else in the Result.
	Excluded []string

	// Ballots is how many ballots entered the count.
	Ballots int

	// Ignored are the voters whose ballots never entered it, sorted: everything
	// they ranked was hidden, unknown to this count, or they ranked nothing.
	// Worth reporting rather than discarding, because "my vote did not count"
	// is a question somebody will ask and the honest answer is a list.
	Ignored []string
}

// Decided reports whether the count produced a single winner.
func (r Result) Decided() bool { return r.Winner != "" }

// String renders a round as one line, for test failures and for anybody
// debugging a count. It is not the site's copy — presentation belongs to the
// web layer, and this package does not import one.
func (r Round) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "round %d (need %d of %d continuing, %d exhausted):",
		r.Number, r.Threshold, r.Continuing, r.Exhausted)
	for _, c := range r.Counts {
		fmt.Fprintf(&b, " %s=%d", c.Movie, c.Votes)
	}

	switch r.Outcome {
	case OutcomeElected:
		fmt.Fprintf(&b, " -> elected %s", r.Elected)
	case OutcomeEliminated:
		fmt.Fprintf(&b, " -> eliminated %s", r.Eliminated)
		if r.TieBreak.Rule != TieBreakNone {
			fmt.Fprintf(&b, " (tie %v broken by %s", r.TieBreak.Tied, r.TieBreak.Rule)
			if r.TieBreak.Rule == TieBreakEarlierRound {
				fmt.Fprintf(&b, " %d", r.TieBreak.Round)
			}
			b.WriteString(")")
		}
		for _, t := range r.Transfers {
			if t.To == "" {
				fmt.Fprintf(&b, " [%d exhausted]", t.Votes)
			} else {
				fmt.Fprintf(&b, " [%d->%s]", t.Votes, t.To)
			}
		}
	case OutcomeDeadHeat:
		fmt.Fprintf(&b, " -> dead heat %v", r.DeadHeat)
	}

	return b.String()
}

// ballot is a Ballot reduced to what the count actually uses: an ordered list
// of candidate IDs that exist and are not hidden.
type ballot struct {
	voter   string
	choices []string
}

// Count runs the election.
//
// It returns a Result for every input, including the degenerate ones — no
// candidates, no ballots, one candidate, every ballot exhausted. None of those
// is an error: a season where every submission was withdrawn is a real season,
// and a tally that returns an error on it is a tally that takes the results
// page down. There is no error return at all, which is deliberate: it means a
// caller cannot handle the sad path by ignoring it.
func Count(candidates []Candidate, ballots []Ballot) Result {
	live, excluded := splitCandidates(candidates)

	result := Result{
		Candidates: slices.Clone(live),
		Excluded:   excluded,
		Tied:       []string{},
		Rounds:     []Round{},
		Ignored:    []string{},
	}
	if len(live) == 0 {
		return result
	}

	counted, ignored := normalise(ballots, live)
	result.Ballots = len(counted)
	result.Ignored = ignored

	// Each iteration either returns or eliminates exactly one candidate, so the
	// count cannot run longer than there are candidates. The bound is written
	// down rather than left as a `for {}` with an argument in a comment: an
	// off-by-one in the elimination logic should end the loop, not hang the
	// request that is rendering the results page.
	//
	// maxRounds is taken BEFORE the loop on purpose. `number <= len(live)` reads
	// correctly and is wrong, because live shrinks by one every time round, so
	// the bound chases the counter and they meet in the middle: a four-horse
	// race stopped after the second elimination with no winner and two
	// candidates still standing.
	maxRounds := len(live)
	for number := 1; number <= maxRounds; number++ {
		round, elected, eliminated := runRound(number, counted, live, result.Rounds)
		result.Rounds = append(result.Rounds, round)

		switch round.Outcome {
		case OutcomeElected:
			result.Winner = elected

			return result
		case OutcomeDeadHeat:
			result.Tied = round.DeadHeat

			return result
		case OutcomeEliminated:
			live = remove(live, eliminated)
		}
	}

	// Unreachable while each round removes one candidate and the last one
	// standing is elected. Returning the rounds so far beats panicking inside
	// a page render.
	return result
}

// splitCandidates returns the candidates that take part, sorted by ID, and the
// hidden ones that do not.
//
// Sorting here is what makes the whole count order-independent: every later
// decision that needs to walk candidates walks this slice rather than a map.
func splitCandidates(candidates []Candidate) (live, excluded []string) {
	live = make([]string, 0, len(candidates))
	excluded = make([]string, 0)

	seen := make(map[string]bool, len(candidates))
	for _, c := range candidates {
		if c.ID == "" || seen[c.ID] {
			continue
		}
		seen[c.ID] = true

		if c.Hidden {
			excluded = append(excluded, c.ID)

			continue
		}
		live = append(live, c.ID)
	}

	slices.Sort(live)
	slices.Sort(excluded)

	return live, excluded
}

// normalise turns Ballots into the ordered candidate lists the count uses, and
// reports the voters whose ballots express nothing.
//
// Three things happen here rather than in the round loop, because they are
// properties of a ballot rather than of a round:
//
//   - preferences are sorted by rank, so a sparse ballot (1, 2, 4) and a dense
//     one (1, 2, 3) become the same list. The secondary sort on the film ID
//     matters only if two preferences somehow share a rank, which the ballot
//     table's primary key forbids — it is here so that malformed input is
//     still counted the same way twice rather than following input order;
//   - preferences for a candidate that is hidden or unknown are dropped, so
//     the ballot falls through to its next surviving choice.
//
// There is deliberately no de-duplication of a film ranked twice. The ballot
// table's unique constraint forbids it, and an earlier version of this function
// stripped repeats anyway on the reasoning that one ballot could otherwise
// transfer to the same film twice. That reasoning is wrong, and a mutation test
// is what showed it: every round assigns a ballot to the FIRST of its choices
// that is still standing, so a repeat is simply never reached, and taking the
// de-duplication out changes no count anywhere. It was removed rather than kept
// as insurance, because unreachable code with a plausible comment above it is
// how the next person comes to believe the guarantee lives here.
// TestAFilmRankedTwiceOnOneBallotCountsOnce pins the behaviour that matters.
func normalise(ballots []Ballot, live []string) (counted []ballot, ignored []string) {
	inLive := make(map[string]bool, len(live))
	for _, id := range live {
		inLive[id] = true
	}

	counted = make([]ballot, 0, len(ballots))
	ignored = make([]string, 0)

	for _, b := range ballots {
		prefs := slices.Clone(b.Preferences)
		slices.SortStableFunc(prefs, func(x, y Preference) int {
			if x.Rank != y.Rank {
				return x.Rank - y.Rank
			}

			return strings.Compare(x.Movie, y.Movie)
		})

		choices := make([]string, 0, len(prefs))
		for _, p := range prefs {
			if !inLive[p.Movie] {
				continue
			}
			choices = append(choices, p.Movie)
		}

		if len(choices) == 0 {
			ignored = append(ignored, b.Voter)

			continue
		}
		counted = append(counted, ballot{voter: b.Voter, choices: choices})
	}

	slices.Sort(ignored)

	return counted, ignored
}

// runRound counts one round and decides what happens at the end of it.
//
// prior is every round already counted, which backwards tie-breaking reads.
func runRound(number int, ballots []ballot, live []string, prior []Round) (round Round, elected, eliminated string) {
	inLive := make(map[string]bool, len(live))
	for _, id := range live {
		inLive[id] = true
	}

	counts := make(map[string]int, len(live))
	for _, id := range live {
		counts[id] = 0
	}

	continuing := 0
	for _, b := range ballots {
		if pick := firstLive(b.choices, inLive); pick != "" {
			counts[pick]++
			continuing++
		}
	}

	round = Round{
		Number:     number,
		Counts:     tallies(live, counts),
		Continuing: continuing,
		Exhausted:  len(ballots) - continuing,
		// A strict majority of the ballots still counting. Integer division is
		// the point: 19 continuing needs 10, and 20 needs 11.
		Threshold: continuing/2 + 1,
	}

	high, low := extremes(live, counts)

	// A candidate on the threshold is necessarily alone up there: two
	// candidates level on v votes put at least 2v ballots into Continuing, and
	// v is never >= (2v)/2 + 1. So this cannot elect one of a tied pair.
	if len(high) == 1 && counts[high[0]] >= round.Threshold {
		round.Outcome = OutcomeElected
		round.Elected = high[0]

		return round, round.Elected, ""
	}

	// The last candidate standing wins even without a majority — by this point
	// everybody else has been eliminated, so there is nobody left to run off
	// against. This is the branch that a heavily exhausted election ends on.
	if len(live) == 1 {
		round.Outcome = OutcomeElected
		round.Elected = live[0]

		return round, round.Elected, ""
	}

	// Everybody level: each of them is at once the leader and the last, so
	// there is no majority to elect and no candidate that can be eliminated for
	// any reason other than an arbitrary one. The fallback is allowed to pick a
	// loser, never a winner, so the count stops here and says so.
	if len(low) == len(live) {
		round.Outcome = OutcomeDeadHeat
		round.DeadHeat = slices.Clone(live)

		return round, "", ""
	}

	round.Outcome = OutcomeEliminated
	round.Eliminated, round.TieBreak = pickForElimination(low, prior)
	round.Transfers = transfersOf(ballots, inLive, round.Eliminated)

	return round, "", round.Eliminated
}

// firstLive is the candidate a ballot counts for: its best-ranked choice that
// is still in the count. Empty when the ballot is exhausted.
func firstLive(choices []string, inLive map[string]bool) string {
	for _, c := range choices {
		if inLive[c] {
			return c
		}
	}

	return ""
}

// tallies renders the round's counts most-votes-first, ties broken by ID.
//
// live is already sorted, so equal votes come out in ID order and the report is
// identical across runs.
func tallies(live []string, counts map[string]int) []Tally {
	out := make([]Tally, 0, len(live))
	for _, id := range live {
		out = append(out, Tally{Movie: id, Votes: counts[id]})
	}
	slices.SortStableFunc(out, func(x, y Tally) int { return y.Votes - x.Votes })

	return out
}

// extremes returns the candidates on the highest count and those on the lowest,
// each sorted by ID because live is.
func extremes(live []string, counts map[string]int) (high, low []string) {
	best, worst := counts[live[0]], counts[live[0]]
	for _, id := range live[1:] {
		best = max(best, counts[id])
		worst = min(worst, counts[id])
	}

	for _, id := range live {
		if counts[id] == best {
			high = append(high, id)
		}
		if counts[id] == worst {
			low = append(low, id)
		}
	}

	return high, low
}

// pickForElimination chooses which of the last-placed candidates goes out.
//
// Backwards tie-breaking: walk the earlier rounds from the most recent to the
// first, narrowing to whoever was lowest in each. The first round that
// separates them settles it. A candidate still in the count today appeared in
// every earlier round, so there is always a count to read.
//
// When they were level in every round there is genuinely nothing left to
// compare, and the lowest ID goes. See the package comment for why arbitrary
// beats random here.
func pickForElimination(low []string, prior []Round) (string, TieBreak) {
	if len(low) == 1 {
		return low[0], TieBreak{Rule: TieBreakNone}
	}

	tie := TieBreak{Tied: slices.Clone(low), Rule: TieBreakLowestID}

	candidates := slices.Clone(low)
	for i := len(prior) - 1; i >= 0; i-- {
		round := prior[i]
		votes := make(map[string]int, len(round.Counts))
		for _, c := range round.Counts {
			votes[c.Movie] = c.Votes
		}

		worst := votes[candidates[0]]
		for _, id := range candidates[1:] {
			worst = min(worst, votes[id])
		}

		narrowed := make([]string, 0, len(candidates))
		for _, id := range candidates {
			if votes[id] == worst {
				narrowed = append(narrowed, id)
			}
		}
		if len(narrowed) == len(candidates) {
			continue
		}

		candidates = narrowed
		if len(candidates) == 1 {
			tie.Rule = TieBreakEarlierRound
			tie.Round = round.Number

			return candidates[0], tie
		}
	}

	// Still level everywhere, or narrowed to a group that is still level.
	// candidates is sorted because low was.
	return candidates[0], tie
}

// transfersOf works out where the eliminated candidate's ballots go next, in
// the same terms a person re-counting by hand would use: so many to each
// surviving film, so many exhausted.
func transfersOf(ballots []ballot, inLive map[string]bool, eliminated string) []Transfer {
	next := make(map[string]int)
	// A copy, because the round that is being reported has not ended yet and
	// firstLive must not see the elimination when deciding which ballots belong
	// to the eliminated candidate.
	after := make(map[string]bool, len(inLive))
	for id, ok := range inLive {
		after[id] = ok
	}
	after[eliminated] = false

	exhausted := 0
	for _, b := range ballots {
		if firstLive(b.choices, inLive) != eliminated {
			continue
		}
		if to := firstLive(b.choices, after); to != "" {
			next[to]++
		} else {
			exhausted++
		}
	}

	to := make([]string, 0, len(next))
	for id := range next {
		to = append(to, id)
	}
	slices.Sort(to)

	out := make([]Transfer, 0, len(to)+1)
	for _, id := range to {
		out = append(out, Transfer{To: id, Votes: next[id]})
	}
	if exhausted > 0 {
		out = append(out, Transfer{To: "", Votes: exhausted})
	}

	return out
}

// remove returns live without id, keeping it sorted.
func remove(live []string, id string) []string {
	out := make([]string, 0, len(live)-1)
	for _, c := range live {
		if c != id {
			out = append(out, c)
		}
	}

	return out
}
