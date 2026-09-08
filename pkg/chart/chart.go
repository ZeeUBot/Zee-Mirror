package chart

import (
	"bytes"
	"fmt"

	"zee-mirror/internal/domain"

	"github.com/wcharczuk/go-chart/v2"
	"github.com/wcharczuk/go-chart/v2/drawing"
)

func GenerateWeeklyStatsChart(stats []domain.DailyStats) ([]byte, error) {
	if len(stats) == 0 {
		return nil, fmt.Errorf("no stats data")
	}

	var values []chart.Value
	for _, day := range stats {
		lbl := day.Date.Format("02/01")
		values = append(values, chart.Value{
			Label: lbl,
			Value: float64(day.TotalTasks),
			Style: chart.Style{
				FillColor:   drawing.ColorFromHex("3498db"),
				StrokeWidth: 0,
			},
		})
	}

	graph := chart.BarChart{
		Title: "Tasks Last 7 Days",
		Background: chart.Style{
			Padding: chart.Box{
				Top:    40,
				Bottom: 20,
			},
		},
		Height:   300,
		BarWidth: 30,
		Bars:     values,
	}

	buffer := bytes.NewBuffer([]byte{})
	err := graph.Render(chart.PNG, buffer)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func GenerateSpeedtestChart(download, upload float64) ([]byte, error) {
	values := []chart.Value{
		{
			Label: "Download",
			Value: download,
			Style: chart.Style{
				FillColor:   drawing.ColorFromHex("2ecc71"),
				StrokeWidth: 0,
			},
		},
		{
			Label: "Upload",
			Value: upload,
			Style: chart.Style{
				FillColor:   drawing.ColorFromHex("e74c3c"),
				StrokeWidth: 0,
			},
		},
	}

	graph := chart.BarChart{
		Title: "Speedtest Result (Mbps)",
		Background: chart.Style{
			Padding: chart.Box{
				Top:    40,
				Bottom: 20,
			},
		},
		Height:   300,
		BarWidth: 60,
		Bars:     values,
	}

	buffer := bytes.NewBuffer([]byte{})
	err := graph.Render(chart.PNG, buffer)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
