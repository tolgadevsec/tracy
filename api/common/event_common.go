package common

import (
	"encoding/json"
	"golang.org/x/net/html"
	"strings"
	"tracy/api/store"
	"tracy/api/types"
	"tracy/log"
)

/*AddEvent is the common functionality to add an event to the database. This function
 * has been separated so both HTTP and websocket servers can use it. */
func AddEvent(tracerID uint, event types.TracerEvent) ([]byte, error) {
	log.Trace.Printf("Adding the following tracer event: %+v, tracerID: %d", event, tracerID)
	var ret []byte
	var err error

	event.TracerID = tracerID
	var overall uint
	if event.DOMContexts, overall, err = getDomContexts(event); err == nil {
		if err = store.DB.Create(&event).Error; err == nil {
			log.Trace.Printf("Successfully added the tracer event to the database.")
			ret, err = json.Marshal(event)
		}

		// Update the tracer with the highest severity
		tracer := types.Tracer{}
		tracer.ID = tracerID
		if err = store.DB.First(&tracer).Error; err == nil {
			tracer.OverallSeverity = overall
			err = store.DB.Save(&tracer).Error
		}
	}

	if err != nil {
		log.Warning.Printf(err.Error())
	}

	return ret, err
}

/*GetEvents is the common functionality for getting all the events for a given tracer ID. */
func GetEvents(tracerID uint) ([]byte, error) {
	log.Trace.Printf("Getting all the events.")
	var ret []byte
	var err error

	tracerEvents := make([]types.TracerEvent, 0)
	if err = store.DB.Joins("JOIN dom_contexts on dom_contexts.tracer_event_id=tracer_events.id").Preload("DOMContexts").Find(&tracerEvents, "tracer_id = ?", tracerID).Error; err == nil {
		log.Trace.Printf("Successfully got the tracer events: %+v", tracerEvents)
		ret, err = json.Marshal(tracerEvents)
	}

	if err != nil {
		log.Warning.Printf(err.Error())
	}

	return ret, err
}

/*addDomContext is the common functionality for adding data to the event context table. */
func getDomContexts(tracerEvent types.TracerEvent) ([]types.DOMContext, uint, error) {
	log.Trace.Printf("Adding the event context for %+v", tracerEvent)
	var err error
	var contexts []types.DOMContext

	var doc *html.Node
	var sev uint = 0
	var ret *uint = &sev
	doc, err = html.Parse(strings.NewReader(tracerEvent.RawEvent))
	if err == nil {
		/* Need to get the tracer string this event is mapped to. */
		var tracer types.Tracer
		if err = store.DB.First(&tracer, "id = ?", tracerEvent.TracerID).Error; err == nil {
			/* Find all instances of the string string and record their appropriate contexts.*/
			getTracerLocation(doc, &contexts, tracer.TracerString, tracerEvent.ID, ret)
			log.Trace.Printf("Got the following DOM contexts from the event: %+v", contexts)
		}
	}

	if err != nil {
		log.Warning.Printf(err.Error())
	}

	return contexts, *ret, err
}

/* Constants used to track the categories for the locations of a tracer string. */
const (
	inAttr = iota
	inText
	inNodeName
	inAttrVal
	inComment
)

/* Helper function that recursively traverses the DOM notes and records any context
 * surrounding a particular string. */
func getTracerLocation(n *html.Node, tracerLocations *[]types.DOMContext, tracer string, tracerEventID uint, highest *uint) {
	var sev uint = 0
	if strings.Contains(n.Data, tracer) {
		if n.Type == html.TextNode {
			log.Trace.Printf("Found Tracer in TextNode. Parent Node: %s, Data: %s", n.Parent.Data, n.Data)
			*tracerLocations = append(*tracerLocations,
				types.DOMContext{
					TracerEventID:    tracerEventID,
					HTMLNodeType:     n.Parent.Data,
					HTMLLocationType: inText,
					EventContext:     n.Data,
					Severity:         0,
				})
		} else if n.Type == html.DocumentNode || n.Type == html.ElementNode || n.Type == html.DoctypeNode {
			log.Trace.Printf("Found Tracer in DomNode. Parent Node: %s, Data: %s", n.Parent.Data, n.Data)

			if n.Parent.Data == "script" {
				sev = 1
			}
			*tracerLocations = append(*tracerLocations,
				types.DOMContext{
					TracerEventID:    tracerEventID,
					HTMLNodeType:     n.Parent.Data,
					HTMLLocationType: inNodeName,
					EventContext:     n.Data,
					Severity:         sev,
				})
		} else {
			//TODO: although, we should care about these cases, there could be a case where the comment could be broken out of
			log.Trace.Printf("Found a comment node. We probably don't care about these as much. Parent node: %s, Data: %s", n.Parent, n.Data)
			sev = 1
			*tracerLocations = append(*tracerLocations,
				types.DOMContext{
					TracerEventID:    tracerEventID,
					HTMLNodeType:     n.Parent.Data,
					HTMLLocationType: inComment,
					EventContext:     n.Data,
					Severity:         sev,
				})
		}
	}

	for _, a := range n.Attr {
		if strings.Contains(a.Key, tracer) {
			sev = 3
			*tracerLocations = append(*tracerLocations,
				types.DOMContext{
					TracerEventID:    tracerEventID,
					HTMLNodeType:     n.Data,
					HTMLLocationType: inAttr,
					EventContext:     a.Key,
					Severity:         sev,
				})
		}

		if strings.Contains(a.Val, tracer) {
			sev = 1
			*tracerLocations = append(*tracerLocations,
				types.DOMContext{
					TracerEventID:    tracerEventID,
					HTMLNodeType:     n.Data,
					HTMLLocationType: inAttrVal,
					EventContext:     a.Val,
					Severity:         sev,
				})
		}
	}

	if sev > *highest {
		*highest = sev
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		getTracerLocation(c, tracerLocations, tracer, tracerEventID, highest)
	}
}
