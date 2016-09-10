// Downsample raw events sent from client to API endpoint

package main

import (
    "fmt"
    "github.com/jrallison/go-workers"
    "github.com/sajari/regression"
    "github.com/influxdata/influxdb/client/v2"
    "github.com/influxdata/influxdb/uuid"
    "time"
    "encoding/json"
    "math"
)

const (
    MyDB = "engagement_development"
    username = "root"
    password = "root"
    avgReadingSpeed = 270.0
)

// queryDB convenience function to query the database
func queryDB(clnt client.Client, cmd string) (res []client.Result, err error) {
    q := client.Query{
        Command:  cmd,
        Database: MyDB,
    }
    if response, err := clnt.Query(q); err == nil {
        if response.Error() != nil {
            return res, response.Error()
        }
        res = response.Results
    } else {
        return res, err
    }
    return res, nil
}

func RegressionWorker(message *workers.Msg) {
    clnt, _ := client.NewHTTPClient(client.HTTPConfig{
        Addr: "http://localhost:8086",
    })
    q := fmt.Sprintf("SELECT * FROM %s group by source_url, session_id", "events")
    res, _ := queryDB(clnt, q)

    bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
        Database:  MyDB,
        Precision: "s",
    })

    for _, response_row := range res {
      for _, results_row := range response_row.Series {
        final_score := 0.0
        time_layout := time.RFC3339

        // regression calculation setup

        r := new(regression.Regression)
        r.SetObserved("Scroll Depth")
        r.SetVar(0, "Elapsed Time")
        r.SetVar(1, "Is Visible")

        // time in viewport calculation and end of content check
        in_viewport := 0.0
        reached_end_of_content := false

        // estimated reading time
        word_count, _ := results_row.Values[0][7].(json.Number).Float64()

        // iterate over values for a given series
        for _, data_row := range results_row.Values {
          if data_row[3] == true && data_row[4] == true {
            in_viewport++
          }
          parsed_time, _ := time.Parse(time_layout, data_row[0].(string))
          starting_time, _ := time.Parse(time_layout, results_row.Values[0][0].(string))
          elapsed_time_in_seconds := parsed_time.Sub(starting_time).Seconds()
          y_position, _ := data_row[9].(json.Number).Float64()
          bottom_of_viewport, _ := data_row[2].(json.Number).Float64()
          if (y_position > bottom_of_viewport && reached_end_of_content != true) {
            reached_end_of_content = true
            _ = reached_end_of_content
          }
          dp := regression.DataPoint(y_position, []float64{elapsed_time_in_seconds, 1})
          r.Train(dp)
        }

        // total viewport time accounting for 2 second delay/gap
        total_in_viewport_time := in_viewport * 2

        // gauge viewport time for user w/ avg reading speed
        estimated_in_viewport_threshold := ((word_count / avgReadingSpeed) * 60.0)

        // run regression
        r.Run()
        estimated_total_read_through := total_in_viewport_time / estimated_in_viewport_threshold
        rsquared_calculation := r.R2

        // Regression Strength - max 50 points
        if math.IsNaN(rsquared_calculation) {
            final_score += 10.0
        } else {
            final_score += math.Min(rsquared_calculation * 50.0, 50.0)
        }

        // InViewPort Time - max 100 Points
        final_score += math.Min(estimated_total_read_through * 100.0, 100.0)

        // for Batching Points
        tags := map[string]string{
            "uuid": uuid.TimeUUID().String(),
            "source_url": results_row.Tags["source_url"],
            "api_key": results_row.Values[0][1].(string),
        }

        fields := map[string]interface{}{
            "reached_end_of_content": reached_end_of_content,
            "total_in_viewport_time": total_in_viewport_time,
            "word_count": word_count,
            "score": final_score,
        }

        pt, err := client.NewPoint(
            "event_scores",
            tags,
            fields,
            time.Now(),
        )

        if err != nil {
            fmt.Println(err)
        }

        bp.AddPoint(pt)
      }
    }
    clnt.Write(bp)
}

func main() {
  workers.Configure(map[string]string{
    // location of redis instance
    "server":  "localhost:6379",
    // instance of the database
    "database":  "0",
    // number of connections to keep open with redis
    "pool":    "30",
    // unique process id for this instance of workers (for proper recovery of inprogress jobs on crash)
    "process": "1",
  })

  // pull messages from "myqueue2" with concurrency of 20
  workers.Process("go_queue", RegressionWorker, 20)

  // stats will be available at http://localhost:8080/stats
  go workers.StatsServer(8080)

  // Blocks until process is told to exit via unix signal
  workers.Run()
}