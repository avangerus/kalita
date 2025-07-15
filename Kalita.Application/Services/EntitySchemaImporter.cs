using System.Text.Json;
using Kalita.Domain.Entities; // если нужны твои модели
using Kalita.Infrastructure.Persistence;

public class EntitySchemaImporter
{
    private readonly AppDbContext _db;
    public EntitySchemaImporter(AppDbContext db) => _db = db;

    public void ImportFromFile(string path)
    {
        var json = File.ReadAllText(path);
        var entities = JsonSerializer.Deserialize<List<EntityTypeDto>>(json);

        if (entities == null)
            throw new Exception("Не удалось десериализовать файл схемы.");

        foreach (var dto in entities)
        {
            // Ищем существующий тип по коду или создаём новый
            var type = _db.EntityTypes.FirstOrDefault(t => t.Code == dto.Code);
            if (type == null)
            {
                type = new EntityType
                {
                    Id = Guid.NewGuid(),
                    Code = dto.Code,
                    DisplayName = dto.Name,
                    Description = dto.Description ?? ""
                };
                _db.EntityTypes.Add(type);
                _db.SaveChanges();
            }
            else
            {
                type.DisplayName = dto.Name;
                type.Description = dto.Description ?? "";
                _db.SaveChanges();
            }

            // Чистим старые поля
            var oldFields = _db.EntityFields.Where(f => f.EntityTypeId == type.Id).ToList();
            _db.EntityFields.RemoveRange(oldFields);
            _db.SaveChanges();

            // Добавляем новые поля
            foreach (var fieldDto in dto.Fields)
            {
                var field = new EntityField
                {
                    Id = Guid.NewGuid(),
                    EntityTypeId = type.Id,
                    Code = fieldDto.Code,
                    DisplayName = fieldDto.Name,
                    FieldType = fieldDto.FieldType,
                    IsRequired = fieldDto.Required, // Исправлено!
                    IsMultiValue = fieldDto.IsCollection, // Или fieldDto.Collection
                    LookupTypeCode = fieldDto.ReferenceTypeCode ?? "",
                    DefaultValue = fieldDto.DefaultValue ?? "",
                    Description = fieldDto.Description ?? "",
                    EnumOptions = fieldDto.EnumOptions != null
                        ? string.Join(",", fieldDto.EnumOptions)
                        : ""
                };
                _db.EntityFields.Add(field);
            }
            _db.SaveChanges();
        }
    }
}
