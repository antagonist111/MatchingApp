package alexmatchen

import (
	"appengine"
	"appengine/urlfetch"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	daysToShow    = 10
	cacheDuration = 10 * time.Hour
	tvmatchenUrl  = "http://www.tvmatchen.nu/"
)

var (
	multipleSpaces = regexp.MustCompile(`\s+`)
	leagues        = []string{"Premier League" /*, "Ligue 1", "Championship", "Allsvenskan"*/}
	dayNames         = map[string]string{
		"Monday":    "Måndag",
		"Tuesday":   "Tisdag",
		"Wednesday": "Onsdag",
		"Thursday":  "Torsdag",
		"Friday":    "Fredag",
		"Saturday":  "Lördag",
		"Sunday":    "Söndag",
	}
	schedule    map[string][]*match
	lastRefresh time.Time
	mu          sync.RWMutex
)

type (
	match struct {
		Name    string
		League  string
		Channel string
		Time    string
	}

	templateData struct {
		Schedule    map[string][]*match
		LastRefresh string
	}
)

// Convert a match to a pretty printable string.
func (m *match) String() string {
	return fmt.Sprintf("* %s %s (%s, %s)", m.Time, m.Name, m.League, m.Channel)
}

// Refresh data from TV-matchen.
func refreshSchedule(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Refreshing schedule..")
	mu.Lock()
	defer func() {
		lastRefresh = time.Now()
		mu.Unlock()
		fmt.Println("..done")
	}()

	// Fetch remote HTML
	c := appengine.NewContext(r)
	client := urlfetch.Client(c)
	resp, err := client.Get(tvmatchenUrl)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Setup parser
	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	schedule = make(map[string][]*match, daysToShow)

	// Parse matches
	days := doc.Find("h2.day-name")
	days.Each(func(i int, s *goquery.Selection) {
		if i >= daysToShow {
			return
		}

		day := s.Find("span.day-name-inner")
		date, _ := day.Attr("id")
		date = strings.Replace(date, "match-day-", "", -1)

		t, _ := time.Parse("2006-01-02", date)
		date = t.Format("2006-01-02 - ") + dayNames[t.Format("Monday")]

		schedule[date] = []*match{}

		matchTable := s.Next()
		matchTable.Find(".sport-name-fotboll").Each(func(mi int, ms *goquery.Selection) {
			name := ms.Find(".match-name").Text()
			league := ms.Find(".league").Text()

			ms.Find(".league").Find("a").Each(func(ai int, as *goquery.Selection) {
				league = strings.Replace(league, as.Text(), "", -1)
			})

			league = strings.Replace(league, "\n", " ", -1)
			league = multipleSpaces.ReplaceAllString(league, " ")
			league = strings.Trim(league, " ")

			interested := false

			// Check if we are interested in this league
			for _, l := range leagues {
				if strings.Contains(league, l) {
					interested = true
					break
				}
			}

			if !interested {
				return
			}

			channelElement := ms.Find(".channel .channel-item")
			channel, _ := channelElement.Attr("title")

			time := ms.Find(".time .field-content").Text()

			schedule[date] = append(schedule[date], &match{
				Name:    name,
				League:  league,
				Channel: channel,
				Time:    time,
			})
		})
	})
}

// Refreshes the schedule if the cache duration has expired.
func refreshScheduleIfNeeded(w http.ResponseWriter, r *http.Request) {
	if elapsed := time.Since(lastRefresh); elapsed > cacheDuration {
		refreshSchedule(w, r)
	}
}

func init() {
	t := template.New("t")
	t, err := t.Parse(htmlTemplate)
	if err != nil {
		panic(err)
	}

	http.HandleFunc("/schedule.json", func(w http.ResponseWriter, r *http.Request) {
		refreshScheduleIfNeeded(w, r)

		js, err := json.Marshal(schedule)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write(js)
	})

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		refreshScheduleIfNeeded(w, r)

		templateData := &templateData{Schedule: schedule, LastRefresh: lastRefresh.Format(time.RFC3339)}
		err = t.Execute(w, templateData)

		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
}

const (
	htmlTemplate = `
<html>
	<head>
		<title>Match på TV:n</title>
	    <meta charset="utf-8" />
	    <link rel="shortcut icon" href="/favicon.ico" type="image/x-icon">
		<link rel="icon" href="/favicon.ico" type="image/x-icon">
	    <link href='http://fonts.googleapis.com/css?family=Open+Sans' rel='stylesheet' type='text/css'>
	    <style type="text/css">
	    	body {
	    		background: #efefef;
	    		color: #333333;
	    		font-family: 'Open Sans', arial;
	    	}

	    	ul {
	    		list-style: none;
	    		margin: 0;
	    		padding: 0;
	    	}

	    	h2 {
	    		margin: 5px 0;
	    	}

	    	li {
	    		font-size: 14px;
	    		padding: 3px 0;
	    	}

	    	em {
	    		font-size: 10px;
	    	}

    		.time {
    			color: #c5752a;
    		}

    		.league-channel {
    			color: #575e5b;
    		}

    		@media all and (max-width: 500px) {
			  .league-channel {
			  	display: block;
			  }
			}
	    </style>
	</head>
	<body>
		Fotboll på TV:n.
		
		{{range $day, $matches := .Schedule}}
			<h2>{{ $day }}</h2>
			<ul>
				{{range $match := $matches}}
					<li>
						<span class="time">{{$match.Time}}</span>
						<span class="name">{{$match.Name}}</span>
						<span class="league-channel">({{$match.League}}, {{$match.Channel}})</span>
					</li>
				{{end}}
			</ul>
		{{end}}

		<em>Uppdaterad {{.LastRefresh}}</em>
	</body>
</html>
`
)
