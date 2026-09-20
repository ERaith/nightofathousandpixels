package tally_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ERaith/nightofathousandpixels/internal/tally"
)

// Every expected winner below was worked out on paper first and the working is
// in the test name. That is the deliverable: a table of elections whose answers
// a person can check without reading tally.go, so that if the implementation
// and the table ever disagree it is an open question which one is wrong rather
// than an assumption that the code is right.

// --- helpers ---------------------------------------------------------------

// runners builds the candidate list. A name ending in "!" is hidden, so a table
// entry shows the withdrawal inline instead of needing a second field.
func runners(ids ...string) []tally.Candidate {
	out := make([]tally.Candidate, 0, len(ids))
	for _, id := range ids {
		if after, ok := strings.CutSuffix(id, "!"); ok {
			out = append(out, tally.Candidate{ID: after, Hidden: true})

			continue
		}
		out = append(out, tally.Candidate{ID: id})
	}

	return out
}

// vote is one ballot, preferences best first, ranked densely from 1.
func vote(voter string, movies ...string) tally.Ballot {
	prefs := make([]tally.Preference, 0, len(movies))
	for i, m := range movies {
		prefs = append(prefs, tally.Preference{Movie: m, Rank: i + 1})
	}

	return tally.Ballot{Voter: voter, Preferences: prefs}
}

// votes is n identical ballots, each with its own voter.
func votes(n int, movies ...string) []tally.Ballot {
	out := make([]tally.Ballot, 0, n)
	for i := range n {
		out = append(out, vote(fmt.Sprintf("%s-%d", strings.Join(movies, ""), i), movies...))
	}

	return out
}

func join(groups ...[]tally.Ballot) []tally.Ballot {
	var out []tally.Ballot
	for _, g := range groups {
		out = append(out, g...)
	}

	return out
}

// show renders a Result for a failure message. Reading five rounds out of a
// %+v of nested structs is not something anybody should have to do at the point
// where they already know something is wrong.
func show(r tally.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "winner=%q tied=%v ballots=%d ignored=%v excluded=%v\n",
		r.Winner, r.Tied, r.Ballots, r.Ignored, r.Excluded)
	for _, round := range r.Rounds {
		fmt.Fprintf(&b, "  %s\n", round)
	}

	return b.String()
}

func countsOf(round tally.Round) map[string]int {
	out := make(map[string]int, len(round.Counts))
	for _, c := range round.Counts {
		out[c.Movie] = c.Votes
	}

	return out
}

// --- the elections ---------------------------------------------------------

type election struct {
	name       string
	candidates []tally.Candidate
	ballots    []tally.Ballot

	wantWinner string
	wantTied   []string
	wantRounds int
	// check is the per-election assertion that the four fields above cannot
	// express: a threshold, a transfer, which candidate a tie-break removed.
	check func(t *testing.T, r tally.Result)
}

func elections() []election {
	return []election{
		{
			// a=3, b=1, c=1 of 5 continuing. Threshold is 5/2+1 = 3. a is on it
			// in the first count, so there is nothing to redistribute and the
			// count is one round long.
			name:       "a first-preference majority wins in round one with no transfers",
			candidates: runners("a", "b", "c"),
			ballots:    join(votes(3, "a"), votes(1, "b"), votes(1, "c")),
			wantWinner: "a",
			wantRounds: 1,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Rounds[0].Threshold; got != 3 {
					t.Errorf("threshold = %d, want 3 (majority of 5)", got)
				}
				if got := countsOf(r.Rounds[0])["a"]; got != 3 {
					t.Errorf("a = %d, want 3", got)
				}
			},
		},
		{
			// R1: a=2, b=2, c=1 of 5. Threshold 3, nobody there. c is alone at
			// the bottom and goes; its one ballot [c,b] moves to b.
			// R2: a=2, b=3 of 5. Threshold 3, b is on it.
			// The point: the round-one leaders were level and the film that
			// won was not one of the two ahead on its own merits, it was the
			// one the eliminated voter preferred.
			name:       "second preferences decide it: b trails 2-2-1 and wins 3-2 on c's transfer",
			candidates: runners("a", "b", "c"),
			ballots:    join(votes(2, "a", "c"), votes(2, "b", "c"), votes(1, "c", "b")),
			wantWinner: "b",
			wantRounds: 2,
			check: func(t *testing.T, r tally.Result) {
				want := []tally.Transfer{{To: "b", Votes: 1}}
				if got := r.Rounds[0].Transfers; !reflect.DeepEqual(got, want) {
					t.Errorf("transfers = %v, want %v", got, want)
				}
				if got := countsOf(r.Rounds[1])["b"]; got != 3 {
					t.Errorf("b in round 2 = %d, want 3", got)
				}
			},
		},
		{
			// R1: a=4, b=3, c=2, d=1 of 10. Threshold 6. d goes, its ballot
			//     [d,b] moves to b.
			// R2: a=4, b=4, c=2 of 10. Threshold 6. c goes, its two ballots
			//     [c,b] move to b.
			// R3: a=6, no -- b=6. Threshold 6, b is on it.
			// a led every round until the last and still lost, which is the
			// whole argument for instant-runoff and the result this group will
			// most want to see worked through.
			name:       "two eliminations: a leads 4-3-2-1 throughout and loses 6-4 at the end",
			candidates: runners("a", "b", "c", "d"),
			ballots: join(
				votes(4, "a"),
				votes(3, "b", "c"),
				votes(2, "c", "b"),
				votes(1, "d", "b"),
			),
			wantWinner: "b",
			wantRounds: 3,
			check: func(t *testing.T, r tally.Result) {
				if got := countsOf(r.Rounds[2]); got["b"] != 6 || got["a"] != 4 {
					t.Errorf("final round = %v, want b=6 a=4", got)
				}
				if got := r.Rounds[0].Eliminated; got != "d" {
					t.Errorf("round 1 eliminated %q, want d", got)
				}
				if got := r.Rounds[1].Eliminated; got != "c" {
					t.Errorf("round 2 eliminated %q, want c", got)
				}
			},
		},
		{
			// R1: a=4, b=3, c=2 of 9 continuing. Threshold 5, nobody. c goes,
			//     and both of its voters ranked nothing else, so two ballots
			//     are exhausted rather than transferred.
			// R2: 7 continuing, 2 exhausted. Threshold is 7/2+1 = 4, NOT the 5
			//     it was in round one -- this is the recomputed-threshold rule
			//     in the package comment, and a=4 is elected on it.
			//
			// Held at a fixed majority of the 9 ballots cast, the count would
			// instead have run a third round, eliminated b, and elected a
			// anyway. That is not a coincidence: a candidate on a majority of
			// the continuing ballots has more than everyone else put together,
			// so no later transfer can overtake them, and the two rules can
			// never crown different films. The rule decides how many rounds
			// get printed and whether the word "majority" on the results page
			// is true -- not who wins.
			name:       "exhausted ballots lower the bar: a is elected on 4 of 7 continuing, not 4 of 9 cast",
			candidates: runners("a", "b", "c"),
			ballots:    join(votes(4, "a"), votes(3, "b"), votes(2, "c")),
			wantWinner: "a",
			wantRounds: 2,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Rounds[0].Threshold; got != 5 {
					t.Errorf("round 1 threshold = %d, want 5", got)
				}
				want := []tally.Transfer{{To: "", Votes: 2}}
				if got := r.Rounds[0].Transfers; !reflect.DeepEqual(got, want) {
					t.Errorf("transfers = %v, want both ballots exhausted", got)
				}
				if got := r.Rounds[1]; got.Threshold != 4 || got.Continuing != 7 || got.Exhausted != 2 {
					t.Errorf("round 2 = threshold %d, continuing %d, exhausted %d; want 4, 7, 2",
						got.Threshold, got.Continuing, got.Exhausted)
				}
			},
		},
		{
			// R1: a=5, b=1, m=4, z=3 of 13. Threshold 7. b goes, its ballot
			//     [b,z] moves to z.
			// R2: a=5, m=4, z=4 of 13. Threshold 7. m and z are level at the
			//     bottom. Round one separates them -- m had 4, z had 3 -- so z
			//     is eliminated. Note that the arbitrary fallback would have
			//     picked the other one, because "m" sorts before "z"; this is
			//     the test that proves backwards tie-breaking is consulted
			//     first and is not just the ID rule by another name.
			//     z's four ballots all exhaust.
			// R3: a=5, m=4 of 9 continuing. Threshold 5, a is on it.
			name:       "a bottom tie is broken by the earlier round: z goes on round one's 3 to m's 4",
			candidates: runners("a", "b", "m", "z"),
			ballots: join(
				votes(5, "a"),
				votes(1, "b", "z"),
				votes(3, "z"),
				votes(4, "m"),
			),
			wantWinner: "a",
			wantRounds: 3,
			check: func(t *testing.T, r tally.Result) {
				second := r.Rounds[1]
				if got := second.Eliminated; got != "z" {
					t.Errorf("eliminated %q, want z (m is what the ID fallback would pick)", got)
				}
				if got := second.TieBreak.Rule; got != tally.TieBreakEarlierRound {
					t.Errorf("tie-break rule = %v, want earlier round", got)
				}
				if got := second.TieBreak.Round; got != 1 {
					t.Errorf("tie-break round = %d, want 1", got)
				}
				if got := second.TieBreak.Tied; !reflect.DeepEqual(got, []string{"m", "z"}) {
					t.Errorf("tied = %v, want [m z]", got)
				}
			},
		},
		{
			// Backwards means MOST RECENT first, and this is the election that
			// can tell the difference. With only one earlier round to look at,
			// walking back and walking forward are the same walk, so a
			// tie-break that reads the rounds in the wrong order passes every
			// other case in this table.
			//
			// R1: lead=8, yeti=4, quest=3, xeno=3, pod=2 of 20. Threshold 11.
			//     pod is alone at the bottom; both its ballots go to xeno.
			// R2: lead=8, xeno=5, yeti=4, quest=3 of 20. Threshold 11. quest
			//     goes: one ballot to yeti, two exhausted.
			// R3: lead=8, xeno=5, yeti=5 of 18 continuing, 2 exhausted.
			//     Threshold 10. xeno and yeti are level.
			//       - round 2, the most recent, had xeno 5 and yeti 4, so
			//         backwards tie-breaking eliminates YETI;
			//       - round 1 had xeno 3 and yeti 4, so a tie-break that
			//         started at the first round would eliminate XENO;
			//       - "xeno" sorts before "yeti", so the arbitrary fallback
			//         would also take XENO.
			//     Only the correct rule produces yeti, which is what this
			//     entry asserts.
			//     All five of yeti's ballots exhaust.
			// R4: lead=8, xeno=5 of 13 continuing. Threshold 7, lead is past it.
			name:       "backwards means most recent first: round 2 eliminates yeti where round 1 would have taken xeno",
			candidates: runners("lead", "pod", "quest", "xeno", "yeti"),
			ballots: join(
				votes(8, "lead"),
				votes(3, "xeno"),
				votes(4, "yeti"),
				votes(2, "pod", "xeno"),
				votes(1, "quest", "yeti"),
				votes(2, "quest"),
			),
			wantWinner: "lead",
			wantRounds: 4,
			check: func(t *testing.T, r tally.Result) {
				third := r.Rounds[2]
				if got := countsOf(third); got["xeno"] != 5 || got["yeti"] != 5 {
					t.Fatalf("round 3 = %v, want xeno and yeti level on 5", got)
				}
				if got := third.Eliminated; got != "yeti" {
					t.Errorf("eliminated %q, want yeti (xeno is what both the forwards walk "+
						"and the ID fallback would pick)", got)
				}
				if got := third.TieBreak.Rule; got != tally.TieBreakEarlierRound {
					t.Errorf("tie-break rule = %v, want earlier round", got)
				}
				if got := third.TieBreak.Round; got != 2 {
					t.Errorf("tie-break read round %d, want 2 (the most recent, not the first)", got)
				}
			},
		},
		{
			// R1: a=2, m=1, z=1 of 4. Threshold 3, a is short of it. m and z
			// are level at the bottom and there is no earlier round to look
			// at, so the arbitrary fallback runs and takes the lower ID. The
			// assertion that matters is not which one went -- it is that the
			// result says out loud that the choice was arbitrary.
			// R2: a=2 of 3 continuing. Threshold 2, a is on it.
			name:       "level in every round: the fallback picks the lower ID and labels itself arbitrary",
			candidates: runners("a", "m", "z"),
			ballots:    join(votes(2, "a"), votes(1, "m"), votes(1, "z")),
			wantWinner: "a",
			wantRounds: 2,
			check: func(t *testing.T, r tally.Result) {
				first := r.Rounds[0]
				if got := first.Eliminated; got != "m" {
					t.Errorf("eliminated %q, want m", got)
				}
				if got := first.TieBreak.Rule; got != tally.TieBreakLowestID {
					t.Errorf("tie-break rule = %v, want lowest id", got)
				}
				if got := first.TieBreak.Round; got != 0 {
					t.Errorf("tie-break round = %d, want 0 (no round settled it)", got)
				}
			},
		},
		{
			// a=2, b=2 of 4. Threshold 3. Neither is on it, and each of them is
			// at once the leader and the last -- there is no candidate that can
			// be eliminated for any reason that is not a coin flip. A dead heat
			// is the honest answer and the schema has a nullable
			// winner_movie_id for it.
			name:       "a two-way dead heat is an outcome, not an error",
			candidates: runners("a", "b"),
			ballots:    join(votes(2, "a"), votes(2, "b")),
			wantWinner: "",
			wantTied:   []string{"a", "b"},
			wantRounds: 1,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Rounds[0].Outcome; got != tally.OutcomeDeadHeat {
					t.Errorf("outcome = %v, want dead heat", got)
				}
				if r.Decided() {
					t.Error("Decided() is true for a dead heat")
				}
			},
		},
		{
			// Three films, one vote each. Every one of them is simultaneously
			// first and last.
			name:       "a three-way dead heat names all three",
			candidates: runners("a", "b", "c"),
			ballots:    join(votes(1, "a"), votes(1, "b"), votes(1, "c")),
			wantWinner: "",
			wantTied:   []string{"a", "b", "c"},
			wantRounds: 1,
		},
		{
			// R1: a=2, b=2, c=1 of 5. Threshold 3. c goes and its voter ranked
			//     nothing else, so that ballot is exhausted.
			// R2: a=2, b=2 of 4 continuing, 1 exhausted. Threshold 3. Level,
			//     and this time the dead heat only appears because a ballot
			//     dropped out of the count.
			name:       "exhaustion can produce a dead heat two rounds in",
			candidates: runners("a", "b", "c"),
			ballots:    join(votes(2, "a"), votes(2, "b"), votes(1, "c")),
			wantWinner: "",
			wantTied:   []string{"a", "b"},
			wantRounds: 2,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Rounds[1].Exhausted; got != 1 {
					t.Errorf("exhausted = %d, want 1", got)
				}
			},
		},
		{
			// a is withdrawn. It has five first preferences -- more than
			// anything else -- and it must not appear in the count at all. The
			// five ballots are not thrown away: each falls through to its next
			// surviving choice, which is b, so b wins 7-2 rather than 2-2.
			// This is nap-8yw: an admin hiding an inappropriate submission
			// mid-season must not then watch it win.
			name:       "a hidden film cannot win even holding the most first preferences",
			candidates: runners("a!", "b", "c"),
			ballots:    join(votes(5, "a", "b"), votes(2, "b"), votes(2, "c")),
			wantWinner: "b",
			wantRounds: 1,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Excluded; !reflect.DeepEqual(got, []string{"a"}) {
					t.Errorf("excluded = %v, want [a]", got)
				}
				if got := r.Candidates; !reflect.DeepEqual(got, []string{"b", "c"}) {
					t.Errorf("candidates = %v, want [b c]", got)
				}
				if got := countsOf(r.Rounds[0]); got["b"] != 7 {
					t.Errorf("b = %d, want 7 (its own 2 plus a's 5)", got["b"])
				}
				for _, round := range r.Rounds {
					if _, ok := countsOf(round)[""]; ok {
						t.Error("an empty candidate reached the count")
					}
					if _, ok := countsOf(round)["a"]; ok {
						t.Errorf("hidden film a appears in round %d", round.Number)
					}
				}
			},
		},
		{
			// Every film this voter ranked was withdrawn, so their ballot has
			// nothing to say about the remaining field. It does not count, and
			// it is reported rather than silently dropped -- "why didn't my
			// vote count" has a real answer and this is where it comes from.
			name:       "a ballot for withdrawn films only is reported, not silently dropped",
			candidates: runners("a!", "b"),
			ballots:    join([]tally.Ballot{vote("dana", "a")}, votes(2, "b")),
			wantWinner: "b",
			wantRounds: 1,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Ignored; !reflect.DeepEqual(got, []string{"dana"}) {
					t.Errorf("ignored = %v, want [dana]", got)
				}
				if got := r.Ballots; got != 2 {
					t.Errorf("ballots = %d, want 2 (dana's never entered the count)", got)
				}
			},
		},
		{
			// One submission and nobody has ranked anything. The threshold is
			// 0/2+1 = 1 and the only candidate has 0, so it is not elected on a
			// majority -- it is elected because there is nobody to run against.
			// Not a crash and not an error: this is the board on the morning of
			// the first of October with one keen person's film on it.
			name:       "one film and no ballots at all still produces that film",
			candidates: runners("a"),
			ballots:    nil,
			wantWinner: "a",
			wantRounds: 1,
			check: func(t *testing.T, r tally.Result) {
				if got := r.Ballots; got != 0 {
					t.Errorf("ballots = %d, want 0", got)
				}
				if got := r.Rounds[0].Threshold; got != 1 {
					t.Errorf("threshold = %d, want 1", got)
				}
			},
		},
		{
			// Everything was withdrawn. No winner, no tie, no rounds, no panic.
			name:       "no candidates at all is an empty result rather than a crash",
			candidates: runners("a!", "b!"),
			ballots:    votes(3, "a"),
			wantWinner: "",
			wantRounds: 0,
			check: func(t *testing.T, r tally.Result) {
				if len(r.Tied) != 0 {
					t.Errorf("tied = %v, want empty", r.Tied)
				}
				if got := r.Excluded; !reflect.DeepEqual(got, []string{"a", "b"}) {
					t.Errorf("excluded = %v, want [a b]", got)
				}
			},
		},
		{
			// Candidates, but not one ballot between them. Everybody is on
			// zero, so everybody is level, so it is a dead heat -- which is the
			// right answer for an election nobody voted in.
			name:       "candidates with no ballots is a dead heat, not a winner",
			candidates: runners("a", "b", "c"),
			ballots:    nil,
			wantWinner: "",
			wantTied:   []string{"a", "b", "c"},
			wantRounds: 1,
		},
	}
}

func TestCount(t *testing.T) {
	for _, e := range elections() {
		t.Run(e.name, func(t *testing.T) {
			got := tally.Count(e.candidates, e.ballots)

			if got.Winner != e.wantWinner {
				t.Errorf("winner = %q, want %q\n%s", got.Winner, e.wantWinner, show(got))
			}
			wantTied := e.wantTied
			if wantTied == nil {
				wantTied = []string{}
			}
			if !reflect.DeepEqual(got.Tied, wantTied) {
				t.Errorf("tied = %v, want %v\n%s", got.Tied, wantTied, show(got))
			}
			// Fatal rather than Errorf: check reads specific rounds by index, so
			// a wrong round count has to stop the subtest here. Letting it
			// through panics, and a panic in one subtest takes down the whole
			// package run and hides every other failure with it -- which is
			// exactly how a suite ends up meaning less than it looks like it
			// does.
			if len(got.Rounds) != e.wantRounds {
				t.Fatalf("rounds = %d, want %d\n%s", len(got.Rounds), e.wantRounds, show(got))
			}
			if e.check != nil {
				e.check(t, got)
			}
		})
	}
}

// Invariants that have to hold for every election in the table, checked
// separately from the hand-computed answers because they are properties of any
// count rather than facts about one.
func TestEveryCountKeepsItsArithmeticStraight(t *testing.T) {
	for _, e := range elections() {
		t.Run(e.name, func(t *testing.T) {
			r := tally.Count(e.candidates, e.ballots)

			for _, round := range r.Rounds {
				if got := round.Continuing + round.Exhausted; got != r.Ballots {
					t.Errorf("round %d: continuing %d + exhausted %d = %d, want %d ballots",
						round.Number, round.Continuing, round.Exhausted, got, r.Ballots)
				}

				sum := 0
				for _, c := range round.Counts {
					sum += c.Votes
					if c.Votes < 0 {
						t.Errorf("round %d: %s has %d votes", round.Number, c.Movie, c.Votes)
					}
				}
				if sum != round.Continuing {
					t.Errorf("round %d: counts sum to %d, want %d continuing",
						round.Number, sum, round.Continuing)
				}

				if !slices.IsSortedFunc(round.Counts, func(x, y tally.Tally) int { return y.Votes - x.Votes }) {
					t.Errorf("round %d: counts are not highest first: %v", round.Number, round.Counts)
				}

				// Every transfer of an eliminated candidate has to account for
				// one of its ballots: this is the sum a person re-counting by
				// hand checks first.
				if round.Outcome != tally.OutcomeEliminated {
					continue
				}
				moved := 0
				for _, tr := range round.Transfers {
					moved += tr.Votes
				}
				if want := countsOf(round)[round.Eliminated]; moved != want {
					t.Errorf("round %d: %s had %d votes but %d transferred",
						round.Number, round.Eliminated, want, moved)
				}
			}

			// A winner and a tie are mutually exclusive.
			if r.Winner != "" && len(r.Tied) > 0 {
				t.Errorf("both a winner (%s) and a tie (%v)", r.Winner, r.Tied)
			}
			// Nothing hidden ever surfaces.
			for _, round := range r.Rounds {
				for _, c := range round.Counts {
					if slices.Contains(r.Excluded, c.Movie) {
						t.Errorf("round %d counts a hidden film %s", round.Number, c.Movie)
					}
				}
			}
			if slices.Contains(r.Excluded, r.Winner) {
				t.Errorf("a hidden film won: %s", r.Winner)
			}
			for _, id := range r.Tied {
				if slices.Contains(r.Excluded, id) {
					t.Errorf("a hidden film is in the tie: %s", id)
				}
			}
		})
	}
}

// The property that matters most, because the failure it guards against is
// invisible: a count that reads a Go map for a decision gives a different
// answer on a different run of the same binary, and the first anybody hears of
// it is two people getting two winners out of the same ballots.
//
// Shuffling the ballots AND each ballot's preferences, then comparing the whole
// Result rather than only the winner, so that a round order or a transfer list
// that moved is caught too.
func TestTheCountDoesNotDependOnTheOrderTheBallotsArriveIn(t *testing.T) {
	for _, e := range elections() {
		t.Run(e.name, func(t *testing.T) {
			want := tally.Count(e.candidates, e.ballots)

			rng := rand.New(rand.NewPCG(1, 2))
			for i := range 200 {
				ballots := make([]tally.Ballot, len(e.ballots))
				for j, b := range e.ballots {
					prefs := slices.Clone(b.Preferences)
					rng.Shuffle(len(prefs), func(x, y int) { prefs[x], prefs[y] = prefs[y], prefs[x] })
					ballots[j] = tally.Ballot{Voter: b.Voter, Preferences: prefs}
				}
				rng.Shuffle(len(ballots), func(x, y int) { ballots[x], ballots[y] = ballots[y], ballots[x] })

				candidates := slices.Clone(e.candidates)
				rng.Shuffle(len(candidates), func(x, y int) { candidates[x], candidates[y] = candidates[y], candidates[x] })

				if got := tally.Count(candidates, ballots); !reflect.DeepEqual(got, want) {
					t.Fatalf("shuffle %d gave a different count\ngot:\n%swant:\n%s", i, show(got), show(want))
				}
			}
		})
	}
}

// nap-m1a: ballot_entry.rank is deliberately sparse and unbounded, so the tally
// has to treat it as an order. (1, 2, 4) and (10, 20, 30) must both behave
// exactly like (1, 2, 3) -- and "exactly" means the whole Result, not just the
// winner, because a threshold computed off a rank number would show up in the
// rounds long before it showed up in a winner.
func TestSparseAndOffsetRanksCountTheSameAsDenseOnes(t *testing.T) {
	candidates := runners("a", "b", "c", "d")

	dense := join(
		votes(4, "a"),
		votes(3, "b", "c"),
		votes(2, "c", "b"),
		votes(1, "d", "b"),
	)

	reranked := func(ranks ...int) []tally.Ballot {
		out := make([]tally.Ballot, 0, len(dense))
		for _, b := range dense {
			prefs := make([]tally.Preference, 0, len(b.Preferences))
			for i, p := range b.Preferences {
				prefs = append(prefs, tally.Preference{Movie: p.Movie, Rank: ranks[i]})
			}
			out = append(out, tally.Ballot{Voter: b.Voter, Preferences: prefs})
		}

		return out
	}

	want := tally.Count(candidates, dense)
	if want.Winner != "b" {
		t.Fatalf("the dense baseline itself is wrong: %s", show(want))
	}

	for _, tc := range []struct {
		name  string
		ranks []int
	}{
		{"gaps in the middle (1, 4, 9)", []int{1, 4, 9}},
		{"starting well above one (10, 20, 30)", []int{10, 20, 30}},
		{"the unbounded value the schema accepts (2, 9999, 10000)", []int{2, 9999, 10000}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tally.Count(candidates, reranked(tc.ranks...)); !reflect.DeepEqual(got, want) {
				t.Errorf("sparse ranks counted differently\ngot:\n%swant:\n%s", show(got), show(want))
			}
		})
	}
}

// One ballot is one vote, even when the ballot is malformed.
//
// The ballot table's unique constraint stops a film being ranked twice, so this
// cannot arrive from Postgres today; the package takes a slice rather than a
// query, so it can arrive from anywhere else. What makes it safe is the
// counting rule itself rather than any defence against it -- a round assigns a
// ballot to the first of its choices still standing and then stops looking, so
// a repeat is never reached. This test is here to keep that true if the
// assignment rule is ever rewritten, not to guard a special case.
func TestAFilmRankedTwiceOnOneBallotCountsOnce(t *testing.T) {
	candidates := runners("a", "b")
	doubled := tally.Ballot{Voter: "greedy", Preferences: []tally.Preference{
		{Movie: "a", Rank: 1},
		{Movie: "a", Rank: 2},
		{Movie: "b", Rank: 3},
	}}

	got := tally.Count(candidates, join([]tally.Ballot{doubled}, votes(1, "b")))

	// One ballot each: a dead heat. A ballot that counted its repeat would give
	// a two votes and an outright win.
	if got.Winner != "" || !reflect.DeepEqual(got.Tied, []string{"a", "b"}) {
		t.Errorf("a duplicate preference was counted twice\n%s", show(got))
	}
	if n := countsOf(got.Rounds[0])["a"]; n != 1 {
		t.Errorf("a = %d, want 1", n)
	}
}

// A preference for a film that is not a candidate at all -- a row left behind
// by a film that was deleted rather than hidden, or a caller passing a partial
// candidate list. It is skipped like a hidden one, and the ballot keeps
// counting for whatever it ranked next.
func TestAPreferenceForAnUnknownFilmIsSkippedNotFatal(t *testing.T) {
	candidates := runners("b", "c")
	got := tally.Count(candidates, join(
		votes(3, "ghost", "b"),
		votes(2, "c"),
	))

	if got.Winner != "b" {
		t.Errorf("winner = %q, want b\n%s", got.Winner, show(got))
	}
	if n := countsOf(got.Rounds[0])["b"]; n != 3 {
		t.Errorf("b = %d, want 3", n)
	}
}

// A run of elections generated at random, checked only against the properties
// that must hold for any count. It is not looking for a particular winner; it
// is looking for a panic, an index out of range, a round loop that does not
// terminate, and a count that disagrees with itself when the input is shuffled.
func TestRandomElectionsHoldTheirInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 11))
	films := []string{"alien", "blob", "carrie", "dawn", "evil", "fly", "gremlins"}

	for i := range 500 {
		candidates := make([]tally.Candidate, 0, len(films))
		for _, f := range films[:2+rng.IntN(len(films)-1)] {
			candidates = append(candidates, tally.Candidate{ID: f, Hidden: rng.IntN(5) == 0})
		}

		ballots := make([]tally.Ballot, 0, 30)
		for v := range rng.IntN(31) {
			depth := 1 + rng.IntN(3)
			picks := slices.Clone(films)
			rng.Shuffle(len(picks), func(x, y int) { picks[x], picks[y] = picks[y], picks[x] })
			ballots = append(ballots, vote(fmt.Sprintf("v%d", v), picks[:depth]...))
		}

		got := tally.Count(candidates, ballots)

		if got.Winner != "" && len(got.Tied) > 0 {
			t.Fatalf("election %d: winner and tie together\n%s", i, show(got))
		}
		if len(got.Candidates) > 0 && len(got.Rounds) == 0 {
			t.Fatalf("election %d: candidates but no rounds\n%s", i, show(got))
		}
		if len(got.Rounds) > len(got.Candidates) {
			t.Fatalf("election %d: %d rounds for %d candidates\n%s",
				i, len(got.Rounds), len(got.Candidates), show(got))
		}
		for _, round := range got.Rounds {
			if round.Continuing+round.Exhausted != got.Ballots {
				t.Fatalf("election %d round %d: ballots do not add up\n%s", i, round.Number, show(got))
			}
		}
		if slices.Contains(got.Excluded, got.Winner) {
			t.Fatalf("election %d: hidden winner\n%s", i, show(got))
		}

		shuffled := slices.Clone(ballots)
		rng.Shuffle(len(shuffled), func(x, y int) { shuffled[x], shuffled[y] = shuffled[y], shuffled[x] })
		if again := tally.Count(candidates, shuffled); !reflect.DeepEqual(again, got) {
			t.Fatalf("election %d is not reproducible\ngot:\n%swant:\n%s", i, show(again), show(got))
		}
	}
}
