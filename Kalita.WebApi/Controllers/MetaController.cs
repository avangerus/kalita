using Microsoft.AspNetCore.Mvc;

[ApiController]
[Route("api/meta")]
public class MetaController : ControllerBase
{
    private readonly EntityTypeMetadataService _meta;

    public MetaController(EntityTypeMetadataService meta)
    {
        _meta = meta;
    }

    [HttpGet("entities")]
    public IActionResult GetEntities()
    {
        return Ok(_meta.GetAll());
    }

    [HttpGet("entities/{code}")]
    public IActionResult GetEntity(string code)
    {
        var entity = _meta.GetByCode(code);
        if (entity == null) return NotFound();
        return Ok(entity);
    }
}
