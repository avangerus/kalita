# The contract desk — what the manager and the directors watch.

dashboard ContractDesk "Contract desk":
    tile "In legal review":   count Contract where status = LegalReview or status = Reviewed
    tile "Awaiting signature": count Contract where status = Negotiation or status = Approved
    tile "Active value":      sum amount Contract where status = Active
    tile "Expiring soon":     count Contract where status = Active and days_left < 30
    tile "By status":         count Contract group by status
