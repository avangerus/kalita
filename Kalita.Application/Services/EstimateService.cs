using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;

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

        public void CreateEstimate(Estimate estimate)
        {
            _db.Estimates.Add(estimate);
            _db.SaveChanges();
        }

        // === ЭТИ МЕТОДЫ ОБЯЗАТЕЛЬНЫ ===
        public bool TryTransition(Guid entityId, string nextStatus, Guid userId, string userFio, string comment, string userRole, out string error)
        {
            var estimate = _db.Estimates.FirstOrDefault(x => x.Id == entityId);
            if (estimate == null)
            {
                error = "Estimate not found.";
                return false;
            }

            if (!_workflow.CanTransition(estimate.Status, nextStatus, estimate, userRole))
            {
                error = "Transition not allowed (wrong role or condition).";
                return false;
            }

            var history = new WorkflowStepHistory
            {
                Id = Guid.NewGuid(),
                EntityId = estimate.Id,
                EntityType = "estimate",
                StepName = _workflow.GetCurrentStep(estimate.Status)?.Name ?? estimate.Status,
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
