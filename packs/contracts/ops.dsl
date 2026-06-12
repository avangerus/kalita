# The contract desk — what the manager and the directors watch.

dashboard ContractDesk "Contract desk":
    tile "In legal review":   count Contract where status = LegalReview or status = Reviewed
    tile "Awaiting signature": count Contract where status = Negotiation or status = Approved
    tile "Active value":      sum amount Contract where status = Active
    tile "Expiring soon":     count Contract where status = Active and days_left < 30
    tile "By status":         count Contract group by status

# The review surface — what the lawyers work through.
dashboard LegalReview "Legal review":
    tile "Open findings":      count Finding where status = Open
    tile "High-risk, open":    count Finding where risk = High and status = Open
    tile "Findings by risk":   count Finding group by risk
    tile "Findings by status": count Finding group by status
