package service

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

const (
	salesLinePattern  = `^(\d{4}-\d{2}-\d{2}),(ord-\d+),(SKU-\d+),(\d+),(\d+\.\d{2})$`
	syntheticDays     = 3
	syntheticLineSize = 6000
)

var salesLineRegexp = regexp.MustCompile(salesLinePattern)

type ReportQuery struct {
	Day      string
	RawQuery string
}

type SalesReport struct {
	Day          string  `json:"day"`
	TotalOrders  int     `json:"total_orders"`
	TotalRevenue float64 `json:"total_revenue"`
	CSV          string  `json:"-"`
	CSVBytes     int     `json:"csv_bytes"`
	Cached       bool    `json:"cached"`
}

type salesLine struct {
	day      string
	orderID  string
	sku      string
	quantity int
	price    float64
}

type ReportService struct {
	rawLines []string
	leaky    bool

	mu    sync.RWMutex
	cache map[string]SalesReport
}

func NewReportService(leaky bool) *ReportService {
	return &ReportService{
		rawLines: generateSalesLines(syntheticLineSize),
		leaky:    leaky,
		cache:    make(map[string]SalesReport),
	}
}

func (s *ReportService) SalesReport(query ReportQuery) SalesReport {
	key := s.cacheKey(query)
	if report, ok := s.cachedReport(key); ok {
		report.Cached = true
		return report
	}

	report := s.build(query.Day)
	s.storeReport(key, report)
	return report
}

func (s *ReportService) CacheSize() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.cache)
}

func (s *ReportService) cacheKey(query ReportQuery) string {
	if s.leaky {
		return query.RawQuery
	}
	return query.Day
}

func (s *ReportService) cachedReport(key string) (SalesReport, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report, ok := s.cache[key]
	return report, ok
}

func (s *ReportService) storeReport(key string, report SalesReport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[key] = report
}

func (s *ReportService) build(day string) SalesReport {
	if s.leaky {
		return s.buildSlow(day)
	}
	return s.buildFast(day)
}

func (s *ReportService) buildSlow(day string) SalesReport {
	report := SalesReport{Day: day}
	csv := ""
	for _, raw := range s.rawLines {
		matcher := regexp.MustCompile(salesLinePattern)
		line, ok := parseSalesLine(matcher, raw)
		if !ok || line.day != day {
			continue
		}
		report.TotalOrders++
		report.TotalRevenue += float64(line.quantity) * line.price
		csv += fmt.Sprintf("%s,%s,%d,%.2f\n", line.orderID, line.sku, line.quantity, line.price)
	}
	report.CSV = csv
	report.CSVBytes = len(csv)
	return report
}

func (s *ReportService) buildFast(day string) SalesReport {
	report := SalesReport{Day: day}
	var csv strings.Builder
	for _, raw := range s.rawLines {
		if !strings.HasPrefix(raw, day) {
			continue
		}
		line, ok := parseSalesLine(salesLineRegexp, raw)
		if !ok {
			continue
		}
		report.TotalOrders++
		report.TotalRevenue += float64(line.quantity) * line.price
		csv.WriteString(line.orderID)
		csv.WriteByte(',')
		csv.WriteString(line.sku)
		csv.WriteByte(',')
		csv.WriteString(strconv.Itoa(line.quantity))
		csv.WriteByte(',')
		csv.WriteString(strconv.FormatFloat(line.price, 'f', 2, 64))
		csv.WriteByte('\n')
	}
	report.CSV = csv.String()
	report.CSVBytes = csv.Len()
	return report
}

func parseSalesLine(matcher *regexp.Regexp, raw string) (salesLine, bool) {
	match := matcher.FindStringSubmatch(raw)
	if match == nil {
		return salesLine{}, false
	}
	quantity, err := strconv.Atoi(match[4])
	if err != nil {
		return salesLine{}, false
	}
	price, err := strconv.ParseFloat(match[5], 64)
	if err != nil {
		return salesLine{}, false
	}
	return salesLine{day: match[1], orderID: match[2], sku: match[3], quantity: quantity, price: price}, true
}

func generateSalesLines(count int) []string {
	lines := make([]string, 0, count)
	for i := range count {
		day := fmt.Sprintf("2026-11-%02d", 5+i%syntheticDays)
		lines = append(lines, fmt.Sprintf("%s,ord-%d,SKU-%d,%d,%d.%02d", day, 100000+i, i%97, 1+i%5, 5+i%40, i%100))
	}
	return lines
}
