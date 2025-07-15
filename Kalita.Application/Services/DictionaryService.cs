using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

public class DictionaryService
{
    private readonly AppDbContext _db;
    public DictionaryService(AppDbContext db) => _db = db;

    public List<DictionaryType> GetTypes() => _db.DictionaryTypes.ToList();
    public (bool Success, DictionaryType? Type, string? Error) CreateType(string code, string name, string? desc)
    {
        if (_db.DictionaryTypes.Any(x => x.Code == code))
            return (false, null, "Type already exists");
        var t = new DictionaryType { Id = Guid.NewGuid(), Code = code, Name = name, Description = desc };
        _db.DictionaryTypes.Add(t); _db.SaveChanges();
        return (true, t, null);
    }
    public List<DictionaryItem> GetItemsByTypeCode(string typeCode)
    {
        var type = _db.DictionaryTypes.FirstOrDefault(x => x.Code == typeCode);
        if (type == null) return new();
        return _db.DictionaryItems.Where(x => x.TypeId == type.Id).ToList();
    }
    public (bool Success, DictionaryItem? Item, string? Error) CreateItem(Guid typeId, string value, string code, string? extraJson, Guid? parentId = null)
    {
        // Проверка уникальности
        if (_db.DictionaryItems.Any(x => x.TypeId == typeId && x.Code == code))
            return (false, null, "Item with this code already exists in this type");

        var it = new DictionaryItem
        {
            Id = Guid.NewGuid(),
            TypeId = typeId,
            Value = value,
            Code = code,
            ExtraJson = extraJson,
            ParentId = parentId
        };
        _db.DictionaryItems.Add(it);
        _db.SaveChanges();
        return (true, it, null);
    }
    public (bool Success, DictionaryItem? Item, string? Error) UpdateItem(Guid id, string value, string code, string? extraJson)
    {
        var it = _db.DictionaryItems.FirstOrDefault(x => x.Id == id);
        if (it == null) return (false, null, "Item not found");
        it.Value = value; it.Code = code; it.ExtraJson = extraJson;
        _db.SaveChanges();
        return (true, it, null);
    }
    public bool DeleteItem(Guid id)
    {
        var it = _db.DictionaryItems.FirstOrDefault(x => x.Id == id);
        if (it == null) return false;
        _db.DictionaryItems.Remove(it); _db.SaveChanges();
        return true;
    }

    public List<DictionaryItem> GetItemsByParent(Guid typeId, Guid? parentId)
    {
        return _db.DictionaryItems.Where(x => x.TypeId == typeId && x.ParentId == parentId).ToList();
    }

}




// namespace Kalita.Application.Services
// {
//     public class DictionaryService
//     {
//         private readonly AppDbContext _db;
//         public DictionaryService(AppDbContext db) => _db = db;

//         public DictionaryType CreateType(string code, string name)
//         {
//             var type = new DictionaryType { Id = Guid.NewGuid(), Code = code, Name = name };
//             _db.DictionaryTypes.Add(type);
//             _db.SaveChanges();
//             return type;
//         }

//         public List<DictionaryType> GetTypes() => _db.DictionaryTypes.ToList();

//         public DictionaryItem CreateItem(Guid typeId, string code, string name, string? value = null, Guid? parentId = null)
//         {
//             var item = new DictionaryItem
//             {
//                 Id = Guid.NewGuid(),
//                 TypeId = typeId,
//                 Code = code,
//                 Name = name,
//                 Value = value,
//                 ParentId = parentId
//             };
//             _db.DictionaryItems.Add(item);
//             _db.SaveChanges();
//             return item;
//         }

//         public List<DictionaryItem> GetItems(Guid typeId)
//         {
//             return _db.DictionaryItems.Where(x => x.TypeId == typeId).ToList();
//         }
//         // Можно добавить методы по коду типа и др.
//     }
// }