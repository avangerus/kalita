// Kalita.Application/Services/WorkflowEntityService.cs

using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.Workflow;

namespace Kalita.Application.Services
{
    /// <summary>
    /// Универсальный сервис для работы с маршрутами и историей согласования любой сущности, поддерживающей Workflow
    /// </summary>
    public class WorkflowEntityService
    {
        private readonly AppDbContext _db;
        private readonly WorkflowEngine _workflow;

        public WorkflowEntityService(AppDbContext db, WorkflowEngine workflow)
        {
            _db = db;
            _workflow = workflow;
        }

        /// <summary>
        /// Универсальный геттер одной сущности по типу и id
        /// </summary>
        public object? Get(string entityType, Guid id)
        {
            return entityType switch
            {
                "Estimate" => _db.Estimates.FirstOrDefault(e => e.Id == id),
                "Expense" => _db.Expenses.FirstOrDefault(e => e.Id == id),
                "Invoice" => _db.Invoices.FirstOrDefault(e => e.Id == id),
                _ => null
            };
        }

        /// <summary>
        /// Получить все сущности по типу
        /// </summary>
        public IEnumerable<object> GetAll(string entityType)
        {
            return entityType switch
            {
                "Estimate" => _db.Estimates.ToList(),
                "Expense" => _db.Expenses.ToList(),
                "Invoice" => _db.Invoices.ToList(),
                _ => new List<object>()
            };
        }

        /// <summary>
        /// Универсальный переход по маршруту для любой сущности
        /// </summary>
        public bool TryTransition(
            string entityType,
            Guid entityId,
            string nextStatus,
            string userId,
            string userFio,
            string comment,
            string userRole,
            out string error)
        {
            var entity = Get(entityType, entityId);
            if (entity == null)
            {
                error = $"{entityType} not found.";
                return false;
            }

            // Получаем статус динамически
            var statusProp = entity.GetType().GetProperty("Status");
            if (statusProp == null)
            {
                error = "Entity does not have 'Status' property.";
                return false;
            }
            var currentStatus = statusProp.GetValue(entity)?.ToString() ?? "";

            if (!_workflow.CanTransition(entityType, currentStatus, nextStatus, entity, userRole))
            {
                error = "Transition not allowed.";
                return false;
            }

            // Сохраняем историю
            var history = new WorkflowStepHistory
            {
                Id = Guid.NewGuid(),
                EntityId = entityId,
                EntityType = entityType,
                StepName = _workflow.GetCurrentStep(entityType, currentStatus)?.Name ?? currentStatus,
                Status = nextStatus,
                UserId = userId,
                UserFio = userFio,
                DateTime = DateTime.UtcNow,
                Action = "Transition",
                Comment = comment,
                Result = "Success"
            };
            _db.WorkflowStepHistories.Add(history);

            // Обновляем статус
            statusProp.SetValue(entity, nextStatus);

            _db.SaveChanges();
            error = "";
            return true;
        }


        // Получить все связанные сущности по EstimateId
        public IEnumerable<Invoice> GetInvoicesByEstimate(Guid estimateId)
        {
            return _db.Invoices.Where(i => i.EstimateId == estimateId).ToList();
        }

        public IEnumerable<Expense> GetExpensesByEstimate(Guid estimateId)
        {
            return _db.Expenses.Where(e => e.EstimateId == estimateId).ToList();
        }

        public IEnumerable<EstimateLine> GetLinesByEstimate(Guid estimateId)
        {
            return _db.EstimateLines.Where(l => l.EstimateId == estimateId).ToList();
        }

        public IEnumerable<EstimateLine> GetLinesByInvoice(Guid invoiceId)
        {
            return _db.EstimateLines.Where(l => l.InvoiceId == invoiceId).ToList();
        }

        public IEnumerable<EstimateLine> GetLinesByExpense(Guid expenseId)
        {
            return _db.EstimateLines.Where(l => l.ExpenseId == expenseId).ToList();
        }

        public (bool Success, Expense? Expense, string Error) CreateExpense(
            string name, decimal amount, string status, Guid estimateId, List<Guid>? lineIds)
        {
            try
            {
                var expense = new Expense
                {
                    Id = Guid.NewGuid(),
                    Name = name,
                    Amount = amount,
                    Status = status,
                    EstimateId = estimateId
                };

                _db.Expenses.Add(expense);

                if (lineIds != null && lineIds.Count > 0)
                {
                    var lines = _db.EstimateLines.Where(l => lineIds.Contains(l.Id)).ToList();
                    foreach (var line in lines)
                    {
                        line.ExpenseId = expense.Id;
                    }
                }

                _db.SaveChanges();
                return (true, expense, "");
            }
            catch (Exception ex)
            {
                return (false, null, ex.Message);
            }
        }


        public (bool Success, Invoice? Invoice, string Error) CreateInvoice(
            string name, decimal amount, string status, Guid estimateId, List<Guid>? lineIds)
        {
            try
            {
                var invoice = new Invoice
                {
                    Id = Guid.NewGuid(),
                    Name = name,
                    Amount = amount,
                    Status = status,
                    EstimateId = estimateId
                };

                _db.Invoices.Add(invoice);

                // Привязка EstimateLine к счету
                if (lineIds != null && lineIds.Count > 0)
                {
                    var lines = _db.EstimateLines.Where(l => lineIds.Contains(l.Id)).ToList();
                    foreach (var line in lines)
                    {
                        line.InvoiceId = invoice.Id;
                    }
                }

                _db.SaveChanges();
                return (true, invoice, "");
            }
            catch (Exception ex)
            {
                return (false, null, ex.Message);
            }
        }


        public object GetWorkflowRoute(string entityType, Guid entityId)
        {
            var entity = Get(entityType, entityId);
            if (entity == null)
                throw new Exception($"{entityType} not found.");

            // Получить текущий статус
            var statusProp = entity.GetType().GetProperty("Status");
            var currentStatus = statusProp?.GetValue(entity)?.ToString() ?? "";

            // Получить workflow-конфиг для этой сущности
            var config = _workflow.GetConfig(entityType);

            // Вернуть информацию для отображения маршрута:
            return new
            {
                CurrentStatus = currentStatus,
                Steps = config.Steps,
                Transitions = config.Transitions
            };
        }


        public (bool Success, Project? Project, string Error) CreateProject(string name, string? description, string createdByUserId)
        {
            try
            {
                var project = new Project
                {
                    Id = Guid.NewGuid(),
                    Name = name,
                    Description = description,
                    CreatedAt = DateTime.UtcNow,
                    CreatedByUserId = createdByUserId
                };
                _db.Projects.Add(project);
                _db.SaveChanges();
                return (true, project, "");
            }
            catch (Exception ex)
            {
                return (false, null, ex.Message);
            }
        }

        public (bool Success, Project? Project, string Error) UpdateProject(Guid id, string name, string? description)
        {
            var project = _db.Projects.FirstOrDefault(x => x.Id == id);
            if (project == null)
                return (false, null, "Project not found");

            project.Name = name;
            project.Description = description;
            _db.SaveChanges();
            return (true, project, "");
        }

        public (bool Success, string Error) DeleteProject(Guid id)
        {
            var project = _db.Projects.FirstOrDefault(x => x.Id == id);
            if (project == null)
                return (false, "Project not found");
            _db.Projects.Remove(project);
            _db.SaveChanges();
            return (true, "");
        }

        public (bool Success, EstimateLine? Line, string Error) CreateEstimateLine(Guid estimateId, string name, decimal quantity, decimal price, EstimateLineType type)
        {
            var estimate = _db.Estimates.FirstOrDefault(e => e.Id == estimateId);
            if (estimate == null)
                return (false, null, "Estimate not found");

            var line = new EstimateLine
            {
                Id = Guid.NewGuid(),
                Name = name,
                Quantity = quantity,
                Price = price,
                Type = type,
                EstimateId = estimateId,
            };
            _db.EstimateLines.Add(line);
            _db.SaveChanges();
            return (true, line, "");
        }


        public (bool Success, Contractor? Contractor, string Error) CreateContractor(string name, string? inn, string? kpp, string? address, string? type, string? phone, string? email)
        {
            try
            {
                var contractor = new Contractor
                {
                    Id = Guid.NewGuid(),
                    Name = name,
                    Inn = inn,
                    Kpp = kpp,
                    Address = address,
                    Type = type,
                    Phone = phone,
                    Email = email
                };
                _db.Contractors.Add(contractor);
                _db.SaveChanges();
                return (true, contractor, "");
            }
            catch (Exception ex)
            {
                return (false, null, ex.Message);
            }
        }

        public (bool Success, Contractor? Contractor, string Error) UpdateContractor(Guid id, string name, string? inn, string? kpp, string? address, string? type, string? phone, string? email)
        {
            var contractor = _db.Contractors.FirstOrDefault(x => x.Id == id);
            if (contractor == null)
                return (false, null, "Contractor not found");

            contractor.Name = name;
            contractor.Inn = inn;
            contractor.Kpp = kpp;
            contractor.Address = address;
            contractor.Type = type;
            contractor.Phone = phone;
            contractor.Email = email;
            _db.SaveChanges();
            return (true, contractor, "");
        }

        public (bool Success, string Error) DeleteContractor(Guid id)
        {
            var contractor = _db.Contractors.FirstOrDefault(x => x.Id == id);
            if (contractor == null)
                return (false, "Contractor not found");
            _db.Contractors.Remove(contractor);
            _db.SaveChanges();
            return (true, "");
        }

        public (bool Success, DictionaryType? Type, string Error) CreateDictionaryType(string code, string name, string? description)
        {
            var type = new DictionaryType
            {
                Id = Guid.NewGuid(),
                Code = code,
                Name = name,
                Description = description
            };
            _db.DictionaryTypes.Add(type);
            _db.SaveChanges();
            return (true, type, "");
        }
        public List<DictionaryType> GetDictionaryTypes()
            => _db.DictionaryTypes.ToList();


        public (bool Success, DictionaryItem? Item, string Error) CreateDictionaryItem(Guid typeId, string value, string code, string? extraJson)
        {
            var item = new DictionaryItem
            {
                Id = Guid.NewGuid(),
                TypeId = typeId,
                Value = value,
                Code = code,
                ExtraJson = extraJson
            };
            _db.DictionaryItems.Add(item);
            _db.SaveChanges();
            return (true, item, "");
        }
        public List<DictionaryItem> GetDictionaryItems(Guid typeId)
            => _db.DictionaryItems.Where(x => x.TypeId == typeId && x.IsActive).ToList();

        public List<DictionaryItem> GetDictionaryItemsByTypeCode(string code)
        {
            var type = _db.DictionaryTypes.FirstOrDefault(t => t.Code == code);
            if (type == null) return new List<DictionaryItem>();
            return _db.DictionaryItems.Where(x => x.TypeId == type.Id && x.IsActive).ToList();
        }


        public EstimateLine AddEstimateLine(Guid estimateId, string name, decimal quantity, decimal price, EstimateLineType type)
        {
            var line = new EstimateLine
            {
                Id = Guid.NewGuid(),
                Name = name,
                Quantity = quantity,
                Price = price,
                EstimateId = estimateId,
                // ...
            };
            _db.EstimateLines.Add(line);
            _db.SaveChanges();
            return line;
        }

        public List<EstimateLine> GetEstimateLines(Guid estimateId)
        {
            return _db.EstimateLines.Where(x => x.EstimateId == estimateId).ToList();
        }


        // Пример для WorkflowEntityService
        public EstimateLine AddLine(Guid estimateId, string name, decimal quantity, decimal price, Guid? unitId = null)
        {
            var line = new EstimateLine
            {
                Id = Guid.NewGuid(),
                Name = name,
                Quantity = quantity,
                Price = price,
                EstimateId = estimateId,
                // UnitId = unitId // если используешь справочник единиц
            };
            _db.EstimateLines.Add(line);
            _db.SaveChanges();
            return line;
        }

        public List<EstimateLine> GetLines(Guid estimateId)
        {
            return _db.EstimateLines.Where(l => l.EstimateId == estimateId).ToList();
        }

        /// <summary>
        /// Получить историю маршрута по сущности
        /// </summary>
        public List<WorkflowStepHistory> GetHistory(string entityType, Guid entityId)
        {
            return _db.WorkflowStepHistories
                .Where(h => h.EntityId == entityId && h.EntityType == entityType)
                .OrderBy(h => h.DateTime)
                .ToList();
        }
    }
}
