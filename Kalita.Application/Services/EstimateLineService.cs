using Kalita.Domain.Entities;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

namespace Kalita.Application.Services
{
    public class EstimateLineService
    {
        private readonly AppDbContext _db;
        public EstimateLineService(AppDbContext db) => _db = db;

        public List<EstimateLineResponse> GetLinesByEstimate(Guid estimateId)
        {
            var lines = _db.EstimateLines
                .Where(x => x.EstimateId == estimateId)
                .ToList();

            // Подтянуть словарь единиц (меньше запросов)
            var unitIds = lines.Where(x => x.UnitId != null).Select(x => x.UnitId.Value).Distinct().ToList();
            var units = _db.DictionaryItems.Where(x => unitIds.Contains(x.Id)).ToDictionary(x => x.Id, x => x.Value);

            return lines.Select(x => new EstimateLineResponse
            {
                Id = x.Id,
                Name = x.Name,
                Quantity = x.Quantity,
                Price = x.Price,
                Total = x.Total,
                Type = x.Type.ToString(),
                UnitId = x.UnitId,
                UnitName = x.UnitId != null && units.ContainsKey(x.UnitId.Value) ? units[x.UnitId.Value] : null
            }).ToList();
        }


        // Получить одну линию
        public EstimateLine? GetLine(Guid id)
        {
            return _db.EstimateLines.FirstOrDefault(x => x.Id == id);
        }

        // Создать новую линию
        public (bool Success, EstimateLine? Line, string? Error) CreateLine(Guid estimateId, string name, decimal qty, decimal price, Guid? unitId)
        {
            // Простейшая валидация
            var estimate = _db.Estimates.FirstOrDefault(x => x.Id == estimateId);
            if (estimate == null) return (false, null, "Estimate not found");

            EstimateLine line = new()
            {
                Id = Guid.NewGuid(),
                EstimateId = estimateId,
                Name = name,
                Quantity = qty,
                Price = price,
                // UnitId = unitId, // если есть в модели!
            };
            _db.EstimateLines.Add(line);
            _db.SaveChanges();
            return (true, line, null);
        }

        // Обновить линию
        public (bool Success, EstimateLine? Line, string? Error) UpdateLine(Guid id, string name, decimal qty, decimal price, Guid? unitId)
        {
            var line = _db.EstimateLines.FirstOrDefault(x => x.Id == id);
            if (line == null) return (false, null, "Line not found");

            line.Name = name;
            line.Quantity = qty;
            line.Price = price;
            // line.UnitId = unitId;
            _db.SaveChanges();
            return (true, line, null);
        }

        // Удалить линию
        public bool DeleteLine(Guid id)
        {
            var line = _db.EstimateLines.FirstOrDefault(x => x.Id == id);
            if (line == null) return false;
            _db.EstimateLines.Remove(line);
            _db.SaveChanges();
            return true;
        }
    }
}