using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

namespace Kalita.Application.Services
{
    public class EstimateService
    {
        private readonly AppDbContext _db;
        private readonly WorkflowEngine _workflow;

        public EstimateService(AppDbContext db, WorkflowEngine workflow)
        {
            _db = db;
            _workflow = workflow;
        }

        public List<Estimate> GetEstimates() => _db.Estimates.ToList();

        public Estimate? GetEstimate(Guid id) => _db.Estimates.FirstOrDefault(x => x.Id == id);

        public Estimate CreateEstimate(string name, decimal amount, decimal margin, string createdByUserId)
        {
            var estimate = new Estimate
            {
                Id = Guid.NewGuid(),
                Name = name,
                Amount = amount,
                Margin = margin,
                Status = "Draft",
                CreatedByUserId = createdByUserId
            };
            _db.Estimates.Add(estimate);
            _db.SaveChanges();
            return estimate;
        }



        public void Update(Estimate estimate)
        {
            _db.Estimates.Update(estimate);
            _db.SaveChanges();
        }

        public IQueryable<Estimate> Query()
        {
            return _db.Estimates.AsQueryable();
        }

        public EstimateLine AddLine(Guid estimateId, string name, decimal amount, EstimateLineType type)
        {
            var line = new EstimateLine
            {
                Id = Guid.NewGuid(),
                EstimateId = estimateId,
                Name = name,
                Amount = amount,
                Type = type
            };
            _db.EstimateLines.Add(line);
            _db.SaveChanges();
            return line;
        }

        public List<EstimateLine> GetLines(Guid estimateId)
        {
            return _db.EstimateLines.Where(x => x.EstimateId == estimateId).ToList();
        }
        public List<Estimate> GetByProject(Guid projectId)
        {
            return _db.Estimates.Where(e => e.ProjectId == projectId).ToList();
        }

        public EstimateReportDto GetReport(Guid estimateId)
        {
            var lines = _db.EstimateLines.Where(l => l.EstimateId == estimateId).ToList();
            var invoices = _db.Invoices.Where(i => i.EstimateId == estimateId).ToList();
            var expenses = _db.Expenses.Where(e => e.EstimateId == estimateId).ToList();

            var incomeTotal = lines.Where(l => l.Type == EstimateLineType.Income).Sum(l => l.Amount);
            var outcomeTotal = lines.Where(l => l.Type == EstimateLineType.Outcome).Sum(l => l.Amount);
            var invoiceTotal = invoices.Sum(i => i.Amount);
            var expenseTotal = expenses.Sum(e => e.Amount);

            return new EstimateReportDto
            {
                EstimateId = estimateId,
                IncomeTotal = incomeTotal,
                OutcomeTotal = outcomeTotal,
                InvoiceTotal = invoiceTotal,
                ExpenseTotal = expenseTotal,
                Balance = incomeTotal - outcomeTotal
            };
        }


        // === ЭТИ МЕТОДЫ ОБЯЗАТЕЛЬНЫ ===
        public bool TryTransition(Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
        {
            var estimate = _db.Estimates.FirstOrDefault(x => x.Id == entityId);
            var entityType = "Estimate";
            if (estimate == null)
            {
                error = "Estimate not found.";
                return false;
            }

            if (!_workflow.CanTransition(entityType, estimate.Status, nextStatus, estimate, userRole))
            {
                error = "Transition not allowed (wrong role or condition).";
                return false;
            }

            var history = new WorkflowStepHistory
            {
                Id = Guid.NewGuid(),
                EntityId = estimate.Id,
                EntityType = "estimate",
                StepName = _workflow.GetCurrentStep(entityType, estimate.Status)?.Name,
                Status = nextStatus,
                UserId = userId,
                UserFio = userFio,
                DateTime = DateTime.UtcNow,
                Action = "Transition",
                Comment = comment,
                Result = "Success"
            };
            _db.WorkflowStepHistories.Add(history);

            estimate.Status = nextStatus;

            _db.SaveChanges();
            error = "";
            return true;
        }

        public List<WorkflowStepHistory> GetHistory(Guid entityId)
        {
            return _db.WorkflowStepHistories
                     .Where(h => h.EntityId == entityId)
                     .OrderBy(h => h.DateTime)
                     .ToList();
        }

    }
}
