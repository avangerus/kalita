
using Kalita.Infrastructure.Persistence;
using Kalita.Domain.Entities;

public class EntityTypeService
{
    private readonly AppDbContext _db;

    public EntityTypeService(AppDbContext db)
    {
        _db = db;
    }

    public List<EntityType> GetAllTypes() =>
        _db.EntityTypes.ToList();

    public EntityType? GetTypeByCode(string code) =>
        _db.EntityTypes.FirstOrDefault(t => t.Code == code);

    public List<EntityField> GetFieldsByTypeCode(string code)
    {
        var type = GetTypeByCode(code);
        if (type == null) return new List<EntityField>();
        return _db.EntityFields.Where(f => f.EntityTypeId == type.Id).ToList();
    }
}
