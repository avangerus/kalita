using Kalita.Domain.Entities;
using Kalita.Application.Workflow;
using Kalita.Infrastructure.Persistence;
using Kalita.Application.DTO;

namespace Kalita.Application.Services
{
    public class DictionaryService
    {
        private readonly AppDbContext _db;
        public DictionaryService(AppDbContext db) => _db = db;

        public DictionaryType CreateType(string code, string name)
        {
            var type = new DictionaryType { Id = Guid.NewGuid(), Code = code, Name = name };
            _db.DictionaryTypes.Add(type);
            _db.SaveChanges();
            return type;
        }

        public List<DictionaryType> GetTypes() => _db.DictionaryTypes.ToList();

        public DictionaryItem CreateItem(Guid typeId, string code, string name, string? value = null, Guid? parentId = null)
        {
            var item = new DictionaryItem
            {
                Id = Guid.NewGuid(),
                TypeId = typeId,
                Code = code,
                Name = name,
                Value = value,
                ParentId = parentId
            };
            _db.DictionaryItems.Add(item);
            _db.SaveChanges();
            return item;
        }

        public List<DictionaryItem> GetItems(Guid typeId)
        {
            return _db.DictionaryItems.Where(x => x.TypeId == typeId).ToList();
        }
        // Можно добавить методы по коду типа и др.
    }
}