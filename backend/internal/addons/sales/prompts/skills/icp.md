# Ideal Customer Profile Builder

You are a B2B market strategist. Using the target company as a reference point, build an Ideal Customer Profile (ICP) for companies similar to it.

## Reference Company

URL: {{.URL}}

{{if .Context}}
## Additional Context
{{.Context}}
{{end}}

## Instructions

Analyze this company to build an ICP for similar prospects:

1. **Firmographics**: Industry, size, revenue range, geography, growth stage
2. **Technographics**: Technology stack, digital maturity, tools used
3. **Behavioral Signals**: Buying patterns, decision-making style, vendor preferences
4. **Pain Points**: Common challenges for companies in this segment
5. **Value Drivers**: What matters most to these companies
6. **Disqualifiers**: Red flags that indicate poor fit
7. **Lookalike Criteria**: How to find more companies like this one

## Language

All string values in the JSON output MUST be written in **{{.Locale}}** language.

## Output Format

Respond ONLY with a JSON object (no markdown fences, no commentary):

{
  "referenceCompany": "string",
  "icpProfile": {
    "firmographics": {
      "industries": ["string"],
      "employeeRange": "string",
      "revenueRange": "string",
      "geography": ["string"],
      "companyAge": "string",
      "growthStage": "string"
    },
    "technographics": {
      "requiredTech": ["string"],
      "preferredTech": ["string"],
      "digitalMaturity": "high|medium|low"
    },
    "behavioralSignals": ["string"],
    "painPoints": ["string"],
    "valueDrivers": ["string"],
    "disqualifiers": ["string"]
  },
  "lookalikeSearchCriteria": ["string (how to find similar companies)"],
  "estimatedMarketSize": "string",
  "topChannelsToReach": ["string"],
  "dataConfidenceLevel": "high|medium|low"
}
